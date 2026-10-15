package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	_ "github.com/go-sql-driver/mysql"
)

//go:embed static
var staticFiles embed.FS

// ─── encryption ──────────────────────────────────────────────────────────────

var encKey []byte

func encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ct), nil
}

func decrypt(ciphertext string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ct := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// ─── server config store ─────────────────────────────────────────────────────

type Server struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Host       string    `json:"host"`
	Port       int       `json:"port"`
	Username   string    `json:"username"`
	Password   string    `json:"password"` // AES-GCM encrypted, base64
	TLSEnabled bool      `json:"tls_enabled"`
	CreatedAt  time.Time `json:"created_at"`
}

// ServerPublic is returned to the UI — no password
type ServerPublic struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Host       string    `json:"host"`
	Port       int       `json:"port"`
	Username   string    `json:"username"`
	TLSEnabled bool      `json:"tls_enabled"`
	CreatedAt  time.Time `json:"created_at"`
}

type serverStore struct {
	mu      sync.RWMutex
	path    string
	servers map[string]*Server
}

func newServerStore(path string) *serverStore {
	s := &serverStore{path: path, servers: make(map[string]*Server)}
	s.load()
	return s
}

func (s *serverStore) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var list []*Server
	if err := json.Unmarshal(data, &list); err != nil {
		log.Printf("warn: could not parse server store: %v", err)
		return
	}
	for _, srv := range list {
		s.servers[srv.ID] = srv
	}
}

func (s *serverStore) save() error {
	list := make([]*Server, 0, len(s.servers))
	for _, srv := range s.servers {
		list = append(list, srv)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}

func (s *serverStore) list() []ServerPublic {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ServerPublic, 0, len(s.servers))
	for _, srv := range s.servers {
		out = append(out, ServerPublic{
			ID: srv.ID, Name: srv.Name, Host: srv.Host,
			Port: srv.Port, Username: srv.Username,
			TLSEnabled: srv.TLSEnabled, CreatedAt: srv.CreatedAt,
		})
	}
	return out
}

func (s *serverStore) get(id string) (*Server, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	srv, ok := s.servers[id]
	return srv, ok
}

func (s *serverStore) add(srv *Server) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.servers[srv.ID] = srv
	return s.save()
}

func (s *serverStore) remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.servers, id)
	return s.save()
}

func (s *serverStore) update(srv *Server) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.servers[srv.ID]; !ok {
		return fmt.Errorf("server not found")
	}
	s.servers[srv.ID] = srv
	return s.save()
}

// ─── MySQL connection pool ────────────────────────────────────────────────────

type connPool struct {
	mu    sync.Mutex
	conns map[string]*sql.DB
}

func (p *connPool) get(srv *Server) (*sql.DB, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if db, ok := p.conns[srv.ID]; ok {
		if db.Ping() == nil {
			return db, nil
		}
		db.Close()
		delete(p.conns, srv.ID)
	}
	pass, err := decrypt(srv.Password)
	if err != nil {
		return nil, fmt.Errorf("decrypt password: %w", err)
	}
	tls := ""
	if srv.TLSEnabled {
		tls = "&tls=true"
	}
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/mysql?parseTime=true%s",
		srv.Username, pass, srv.Host, srv.Port, tls)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	p.conns[srv.ID] = db
	return db, nil
}

func (p *connPool) evict(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if db, ok := p.conns[id]; ok {
		db.Close()
		delete(p.conns, id)
	}
}

// ─── app wiring ──────────────────────────────────────────────────────────────

var (
	store *serverStore
	pool  *connPool
)

type dbHandlerFunc func(*gin.Context, *sql.DB)

func withDB(fn dbHandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		srv, ok := store.get(c.Param("id"))
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
			return
		}
		db, err := pool.get(srv)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "cannot connect to MySQL: " + err.Error()})
			return
		}
		fn(c, db)
	}
}

func main() {
	keyStr := mustEnv("ENCRYPTION_KEY")
	if len(keyStr) < 32 {
		log.Fatal("ENCRYPTION_KEY must be at least 32 characters")
	}
	encKey = []byte(keyStr[:32])

	dataDir := getEnv("DATA_DIR", "/data")
	if err := os.MkdirAll(dataDir, 0750); err != nil {
		log.Fatalf("mkdir %s: %v", dataDir, err)
	}
	store = newServerStore(filepath.Join(dataDir, "servers.json"))
	pool = &connPool{conns: make(map[string]*sql.DB)}

	adminUser := getEnv("ADMIN_USER", "admin")
	adminPass := mustEnv("ADMIN_PASS")
	accounts := gin.Accounts{adminUser: adminPass}

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	r.SetTrustedProxies(nil)

	auth := gin.BasicAuth(accounts)

	r.GET("/", auth, func(c *gin.Context) { c.Redirect(http.StatusFound, "/ui/") })
	r.GET("/ui/*path", auth, serveStatic)

	api := r.Group("/api", auth)

	// Server registry
	api.GET("/servers", handleListServers)
	api.POST("/servers", handleAddServer)
	api.DELETE("/servers/:id", handleRemoveServer)
	api.PUT("/servers/:id", handleUpdateServer)
	api.POST("/servers/:id/test", handleTestServer)

	// Per-server MySQL operations
	api.GET("/servers/:id/users", withDB(handleListUsers))
	api.POST("/servers/:id/users", withDB(handleCreateUser))
	api.DELETE("/servers/:id/users", withDB(handleDeleteUser))
	api.GET("/servers/:id/grants", withDB(handleGetGrants))
	api.POST("/servers/:id/grants", withDB(handleGrantPermission))
	api.POST("/servers/:id/revoke", withDB(handleRevokePermission))
	api.GET("/servers/:id/roles", withDB(handleListRoles))
	api.POST("/servers/:id/roles", withDB(handleCreateRole))
	api.DELETE("/servers/:id/roles", withDB(handleDeleteRole))
	api.POST("/servers/:id/assign-role", withDB(handleAssignRole))
	api.POST("/servers/:id/revoke-role", withDB(handleRevokeRole))
	api.GET("/servers/:id/databases", withDB(handleListDatabases))
	api.GET("/servers/:id/version", withDB(handleGetVersion))

	log.Printf("listening on :%s", getEnv("PORT", "8080"))
	log.Fatal(r.Run(":" + getEnv("PORT", "8080")))
}

// ─── static file handler ─────────────────────────────────────────────────────

func serveStatic(c *gin.Context) {
	p := c.Param("path")
	if p == "/" || p == "" {
		p = "/index.html"
	}
	data, err := staticFiles.ReadFile("static" + p)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Data(http.StatusOK, mimeFor(p), data)
}

// ─── server registry handlers ─────────────────────────────────────────────────

func handleListServers(c *gin.Context) {
	c.JSON(http.StatusOK, store.list())
}

func handleAddServer(c *gin.Context) {
	var req struct {
		Name       string `json:"name" binding:"required"`
		Host       string `json:"host" binding:"required"`
		Port       int    `json:"port"`
		Username   string `json:"username" binding:"required"`
		Password   string `json:"password" binding:"required"`
		TLSEnabled bool   `json:"tls_enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Port == 0 {
		req.Port = 3306
	}
	enc, err := encrypt(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "encrypt: " + err.Error()})
		return
	}
	srv := &Server{
		ID:         uuid.New().String(),
		Name:       req.Name,
		Host:       req.Host,
		Port:       req.Port,
		Username:   req.Username,
		Password:   enc,
		TLSEnabled: req.TLSEnabled,
		CreatedAt:  time.Now(),
	}
	if err := store.add(srv); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ServerPublic{
		ID: srv.ID, Name: srv.Name, Host: srv.Host,
		Port: srv.Port, Username: srv.Username,
		TLSEnabled: srv.TLSEnabled, CreatedAt: srv.CreatedAt,
	})
}

func handleRemoveServer(c *gin.Context) {
	id := c.Param("id")
	pool.evict(id)
	if err := store.remove(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "server removed"})
}

func handleUpdateServer(c *gin.Context) {
	id := c.Param("id")
	existing, ok := store.get(id)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}
	var req struct {
		Name       string `json:"name" binding:"required"`
		Host       string `json:"host" binding:"required"`
		Port       int    `json:"port"`
		Username   string `json:"username" binding:"required"`
		Password   string `json:"password"`
		TLSEnabled bool   `json:"tls_enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Port == 0 {
		req.Port = 3306
	}
	encPass := existing.Password
	if req.Password != "" {
		enc, err := encrypt(req.Password)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "encrypt: " + err.Error()})
			return
		}
		encPass = enc
	}
	srv := &Server{
		ID:         existing.ID,
		Name:       req.Name,
		Host:       req.Host,
		Port:       req.Port,
		Username:   req.Username,
		Password:   encPass,
		TLSEnabled: req.TLSEnabled,
		CreatedAt:  existing.CreatedAt,
	}
	if err := store.update(srv); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	pool.evict(id)
	c.JSON(http.StatusOK, ServerPublic{
		ID: srv.ID, Name: srv.Name, Host: srv.Host,
		Port: srv.Port, Username: srv.Username,
		TLSEnabled: srv.TLSEnabled, CreatedAt: srv.CreatedAt,
	})
}

func handleTestServer(c *gin.Context) {
	srv, ok := store.get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}
	pool.evict(srv.ID) // force fresh connection
	_, err := pool.get(srv)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ─── MySQL user handlers ──────────────────────────────────────────────────────

type userRow struct {
	Host       string `json:"host"`
	Username   string `json:"username"`
	IsLocked   bool   `json:"is_locked"`
}

func handleListUsers(c *gin.Context, db *sql.DB) {
	rows, err := db.Query(`SELECT Host, User, COALESCE(account_locked,'N') FROM mysql.user WHERE User != '' ORDER BY User, Host`)
	if err != nil {
		apiErr(c, err)
		return
	}
	defer rows.Close()
	var out []userRow
	for rows.Next() {
		var u userRow
		var locked string
		rows.Scan(&u.Host, &u.Username, &locked)
		u.IsLocked = locked == "Y"
		out = append(out, u)
	}
	if out == nil {
		out = []userRow{}
	}
	c.JSON(http.StatusOK, out)
}

func handleCreateUser(c *gin.Context, db *sql.DB) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Host     string `json:"host" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	q := fmt.Sprintf("CREATE USER '%s'@'%s' IDENTIFIED BY '%s'", esc(req.Username), esc(req.Host), esc(req.Password))
	if _, err := db.Exec(q); err != nil {
		apiErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "user created"})
}

func handleDeleteUser(c *gin.Context, db *sql.DB) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Host     string `json:"host" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	q := fmt.Sprintf("DROP USER IF EXISTS '%s'@'%s'", esc(req.Username), esc(req.Host))
	if _, err := db.Exec(q); err != nil {
		apiErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "user deleted"})
}

// ─── MySQL grants handlers ────────────────────────────────────────────────────

func handleGetGrants(c *gin.Context, db *sql.DB) {
	username, host := c.Query("username"), c.Query("host")
	if username == "" || host == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username and host required"})
		return
	}
	rows, err := db.Query(fmt.Sprintf("SHOW GRANTS FOR '%s'@'%s'", esc(username), esc(host)))
	if err != nil {
		apiErr(c, err)
		return
	}
	defer rows.Close()
	var grants []string
	for rows.Next() {
		var g string
		rows.Scan(&g)
		grants = append(grants, g)
	}
	if grants == nil {
		grants = []string{}
	}
	c.JSON(http.StatusOK, grants)
}

func handleGrantPermission(c *gin.Context, db *sql.DB) {
	var req struct {
		Username  string `json:"username" binding:"required"`
		Host      string `json:"host" binding:"required"`
		Privilege string `json:"privilege" binding:"required"`
		Database  string `json:"database" binding:"required"`
		Table     string `json:"table"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	table := req.Table
	if table == "" {
		table = "*"
	}
	q := fmt.Sprintf("GRANT %s ON %s TO '%s'@'%s'",
		safePriv(req.Privilege), fmtTarget(req.Database, table),
		esc(req.Username), esc(req.Host))
	if _, err := db.Exec(q); err != nil {
		apiErr(c, err)
		return
	}
	db.Exec("FLUSH PRIVILEGES")
	c.JSON(http.StatusOK, gin.H{"message": "granted"})
}

func handleRevokePermission(c *gin.Context, db *sql.DB) {
	var req struct {
		Username  string `json:"username" binding:"required"`
		Host      string `json:"host" binding:"required"`
		Privilege string `json:"privilege" binding:"required"`
		Database  string `json:"database" binding:"required"`
		Table     string `json:"table"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	table := req.Table
	if table == "" {
		table = "*"
	}
	q := fmt.Sprintf("REVOKE %s ON %s FROM '%s'@'%s'",
		safePriv(req.Privilege), fmtTarget(req.Database, table),
		esc(req.Username), esc(req.Host))
	if _, err := db.Exec(q); err != nil {
		apiErr(c, err)
		return
	}
	db.Exec("FLUSH PRIVILEGES")
	c.JSON(http.StatusOK, gin.H{"message": "revoked"})
}

// ─── MySQL role handlers (MySQL 8+) ──────────────────────────────────────────

type roleRow struct {
	Host string `json:"host"`
	Name string `json:"name"`
}

func handleListRoles(c *gin.Context, db *sql.DB) {
	rows, err := db.Query(`SELECT Host, User FROM mysql.user WHERE account_locked='Y' AND User!='' ORDER BY User`)
	if err != nil {
		c.JSON(http.StatusOK, []roleRow{})
		return
	}
	defer rows.Close()
	var out []roleRow
	for rows.Next() {
		var r roleRow
		rows.Scan(&r.Host, &r.Name)
		out = append(out, r)
	}
	if out == nil {
		out = []roleRow{}
	}
	c.JSON(http.StatusOK, out)
}

func handleCreateRole(c *gin.Context, db *sql.DB) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if _, err := db.Exec(fmt.Sprintf("CREATE ROLE IF NOT EXISTS '%s'", esc(req.Name))); err != nil {
		apiErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "role created"})
}

func handleDeleteRole(c *gin.Context, db *sql.DB) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if _, err := db.Exec(fmt.Sprintf("DROP ROLE IF EXISTS '%s'", esc(req.Name))); err != nil {
		apiErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "role deleted"})
}

func handleAssignRole(c *gin.Context, db *sql.DB) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Host     string `json:"host" binding:"required"`
		Role     string `json:"role" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	q := fmt.Sprintf("GRANT '%s' TO '%s'@'%s'", esc(req.Role), esc(req.Username), esc(req.Host))
	if _, err := db.Exec(q); err != nil {
		apiErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "role assigned"})
}

func handleRevokeRole(c *gin.Context, db *sql.DB) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Host     string `json:"host" binding:"required"`
		Role     string `json:"role" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	q := fmt.Sprintf("REVOKE '%s' FROM '%s'@'%s'", esc(req.Role), esc(req.Username), esc(req.Host))
	if _, err := db.Exec(q); err != nil {
		apiErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "role revoked"})
}

func handleGetVersion(c *gin.Context, db *sql.DB) {
	var version string
	if err := db.QueryRow("SELECT VERSION()").Scan(&version); err != nil {
		apiErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"version": version})
}

func handleListDatabases(c *gin.Context, db *sql.DB) {
	rows, err := db.Query("SHOW DATABASES")
	if err != nil {
		apiErr(c, err)
		return
	}
	defer rows.Close()
	var dbs []string
	for rows.Next() {
		var d string
		rows.Scan(&d)
		dbs = append(dbs, d)
	}
	if dbs == nil {
		dbs = []string{}
	}
	c.JSON(http.StatusOK, dbs)
}

// ─── utilities ───────────────────────────────────────────────────────────────

func esc(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	return strings.ReplaceAll(s, "'", "\\'")
}

func fmtTarget(db, table string) string {
	if db == "*" {
		return "*.*"
	}
	dbQ := "`" + strings.ReplaceAll(db, "`", "``") + "`"
	if table == "*" {
		return dbQ + ".*"
	}
	return dbQ + ".`" + strings.ReplaceAll(table, "`", "``") + "`"
}

var privAllowlist = map[string]bool{
	"ALL PRIVILEGES": true, "ALL": true,
	"SELECT": true, "INSERT": true, "UPDATE": true, "DELETE": true,
	"CREATE": true, "DROP": true, "ALTER": true, "INDEX": true,
	"REFERENCES": true, "EXECUTE": true, "CREATE VIEW": true,
	"SHOW VIEW": true, "TRIGGER": true, "CREATE ROUTINE": true,
	"ALTER ROUTINE": true, "LOCK TABLES": true,
	"CREATE TEMPORARY TABLES": true, "GRANT OPTION": true,
	"RELOAD": true, "REPLICATION SLAVE": true,
	"REPLICATION CLIENT": true, "PROCESS": true,
	"SHOW DATABASES": true, "SUPER": true, "USAGE": true,
	"EVENT": true, "FILE": true, "CREATE USER": true,
}

func safePriv(p string) string {
	p = strings.ToUpper(strings.TrimSpace(p))
	if privAllowlist[p] {
		return p
	}
	return "USAGE"
}

func mimeFor(path string) string {
	m := mime.TypeByExtension(filepath.Ext(path))
	if m == "" {
		return "application/octet-stream"
	}
	return m
}

func apiErr(c *gin.Context, err error) {
	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
}

func getEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		log.Fatalf("required environment variable %s is not set", k)
	}
	return v
}
