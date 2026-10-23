package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/yumestk/VedioMind-go/internal/ai"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yumestk/VedioMind-go/internal/platform"
)

type Server struct {
	AI        *ai.Client
	workers   sync.WaitGroup
	DB        *sql.DB
	Config    Config
	dummyHash []byte
	Resolver  *platform.Resolver
}

type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

func New(db *sql.DB, cfg Config) *Server {
	return &Server{DB: db, Config: cfg, dummyHash: passwordDummy(), Resolver: platform.New(os.Getenv("YTDLP_BINARY"), os.Getenv("YTDLP_PROXY")), AI: &ai.Client{URL: envString("AI_BASE_URL", "https://api.deepseek.com"), Key: os.Getenv("AI_API_KEY"), Model: os.Getenv("AI_MODEL"), Budget: envInt("AI_CONTEXT_BUDGET", 48000), Output: envInt("AI_OUTPUT_TOKENS", 4096), HTTP: &http.Client{Timeout: 120 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}}
}
func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func fail(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"code": code, "message": message, "requestId": c.GetString("requestId")})
}
func internal(c *gin.Context, err error) {
	slog.Error("request failed", "requestId", c.GetString("requestId"), "error", err)
	fail(c, 500, "INTERNAL_ERROR", "服务暂时不可用，请稍后重试")
}
func decode(c *gin.Context, v any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		fail(c, 400, "INVALID_INPUT", "请求内容无效")
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		fail(c, 400, "INVALID_INPUT", "请求内容无效")
		return false
	}
	return true
}
func (s *Server) cookie(c *gin.Context, name, value string, age int, httpOnly bool) {
	http.SetCookie(c.Writer, &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: age, HttpOnly: httpOnly, Secure: s.Config.SecureCookie, SameSite: http.SameSiteLaxMode})
}
func (s *Server) Routes() *gin.Engine {
	r := gin.New()
	if err := r.SetTrustedProxies(s.Config.TrustedProxies); err != nil {
		panic(err)
	}
	r.Use(func(c *gin.Context) {
		id := randomToken()
		c.Set("requestId", id)
		c.Header("X-Request-ID", id)
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Cache-Control", "no-store")
		c.Next()
	}, gin.CustomRecovery(func(c *gin.Context, _ any) { fail(c, 500, "INTERNAL_ERROR", "服务暂时不可用") }))
	r.GET("/api/health/live", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	r.GET("/api/health/ready", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := s.DB.PingContext(ctx); err != nil {
			fail(c, 503, "DATABASE_UNAVAILABLE", "数据库尚未就绪")
			return
		}
		c.JSON(200, gin.H{"status": "ready"})
	})
	api := r.Group("/api")
	api.Use(s.csrf)
	api.GET("/auth/me", s.me)
	api.POST("/auth/register", s.register)
	api.POST("/auth/login", s.login)
	api.POST("/auth/logout", s.logout)
	api.GET("/media", s.listMedia)
	api.POST("/media/import-url", s.importMedia)
	api.GET("/media/:id", s.getMedia)
	api.DELETE("/media/:id", s.deleteMedia)
	api.GET("/media/:id/transcript", s.getTranscript)
	api.GET("/media/:id/analysis", s.analysisResult)
	api.GET("/media/:id/chapters", s.getChapters)
	api.POST("/media/:id/analyses", s.startAnalysis)
	api.GET("/media/:id/analysis-jobs/active", s.activeJob)
	api.GET("/analysis-jobs/:jobId", s.getJob)
	api.GET("/media/:id/conversations", s.listConversations)
	api.GET("/conversations/:conversationId/messages", s.getMessages)
	api.POST("/media/:id/questions", s.askQuestion)
	r.NoRoute(s.notFoundOrSPA)
	return r
}
func (s *Server) csrf(c *gin.Context) {
	if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
		token, err := c.Cookie("vm_csrf")
		if err != nil || len(token) != 64 {
			token = randomToken()
			s.cookie(c, "vm_csrf", token, 86400, false)
		}
		c.Header("X-CSRF-Token", token)
		c.Next()
		return
	}
	cookie, err := c.Cookie("vm_csrf")
	header := c.GetHeader("X-CSRF-Token")
	if c.GetHeader("Origin") != s.Config.Origin || err != nil || len(cookie) != 64 || subtle.ConstantTimeCompare([]byte(cookie), []byte(header)) != 1 {
		fail(c, 403, "CSRF_REJECTED", "请求来源或防伪令牌无效，请刷新页面")
		return
	}
	if !strings.HasPrefix(c.GetHeader("Content-Type"), "application/json") {
		fail(c, 415, "JSON_REQUIRED", "请使用 JSON 请求")
		return
	}
	c.Next()
}
func (s *Server) current(c *gin.Context) (User, bool) {
	var u User
	token, err := c.Cookie("vm_session")
	if err != nil || len(token) != 64 {
		return u, false
	}
	err = s.DB.QueryRowContext(c.Request.Context(), `SELECT u.id::text,u.email FROM sessions s JOIN users u ON u.id=s.user_id WHERE token_hash=$1 AND expires_at>clock_timestamp()`, digest(token)).Scan(&u.ID, &u.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return u, false
	}
	if err != nil {
		internal(c, err)
		return u, false
	}
	return u, true
}
func (s *Server) requireUser(c *gin.Context) (User, bool) {
	u, ok := s.current(c)
	if !ok && !c.IsAborted() {
		fail(c, 401, "UNAUTHENTICATED", "请先登录")
	}
	return u, ok
}
func (s *Server) me(c *gin.Context) {
	u, ok := s.requireUser(c)
	if ok {
		c.JSON(200, gin.H{"user": u})
	}
}
func (s *Server) logout(c *gin.Context) {
	if token, err := c.Cookie("vm_session"); err == nil {
		if _, err = s.DB.ExecContext(c.Request.Context(), `DELETE FROM sessions WHERE token_hash=$1`, digest(token)); err != nil {
			internal(c, err)
			return
		}
	}
	s.cookie(c, "vm_session", "", -1, true)
	c.Status(204)
}

// Allow uses a database-time fixed window, shared by all application instances.
func (s *Server) allow(ctx context.Context, scope string, limit, seconds int) (bool, error) {
	var count int
	err := s.DB.QueryRowContext(ctx, `INSERT INTO rate_limit_counters(scope,window_start,count) VALUES($1,floor(extract(epoch from clock_timestamp())/$2)::bigint,1) ON CONFLICT(scope,window_start) DO UPDATE SET count=rate_limit_counters.count+1 RETURNING count`, scope, seconds).Scan(&count)
	return count <= limit, err
}
