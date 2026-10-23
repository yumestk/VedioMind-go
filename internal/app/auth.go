package app

import (
	"database/sql"
	"errors"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
)

const passwordCost = 12

func passwordDummy() []byte {
	b, err := bcrypt.GenerateFromPassword([]byte("never-a-valid-account-password"), passwordCost)
	if err != nil {
		panic(err)
	}
	return b
}

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) authInput(c *gin.Context) (credentials, bool) {
	var v credentials
	if !decode(c, &v) {
		return v, false
	}
	v.Email = strings.ToLower(strings.TrimSpace(v.Email))
	a, err := mail.ParseAddress(v.Email)
	if err != nil || a.Address != v.Email || len(v.Email) > 254 || utf8.RuneCountInString(v.Password) < 10 || len(v.Password) > 72 {
		fail(c, 400, "INVALID_CREDENTIALS", "请输入有效邮箱和 10–72 字节密码")
		return v, false
	}
	for _, scope := range []string{"auth:ip:" + c.ClientIP(), "auth:account:" + digest(v.Email)} {
		ok, err := s.allow(c.Request.Context(), scope, 10, 60)
		if err != nil {
			internal(c, err)
			return v, false
		}
		if !ok {
			c.Header("Retry-After", "60")
			fail(c, 429, "AUTH_RATE_LIMITED", "尝试次数过多，请稍后再试")
			return v, false
		}
	}
	return v, true
}
func (s *Server) register(c *gin.Context) {
	v, ok := s.authInput(c)
	if !ok {
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(v.Password), passwordCost)
	if err != nil {
		internal(c, err)
		return
	}
	tx, err := s.DB.BeginTx(c.Request.Context(), nil)
	if err != nil {
		internal(c, err)
		return
	}
	defer tx.Rollback()
	var u User
	err = tx.QueryRowContext(c.Request.Context(), `INSERT INTO users(email,password_hash) VALUES($1,$2) RETURNING id::text,email`, v.Email, string(hash)).Scan(&u.ID, &u.Email)
	if err != nil {
		var p *pgconn.PgError
		if errors.As(err, &p) && p.Code == "23505" {
			fail(c, 409, "ACCOUNT_UNAVAILABLE", "该账号无法注册")
			return
		}
		internal(c, err)
		return
	}
	token := randomToken()
	if _, err = tx.ExecContext(c.Request.Context(), `INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,$2,clock_timestamp()+interval '7 days')`, digest(token), u.ID); err != nil {
		internal(c, err)
		return
	}
	if err = tx.Commit(); err != nil {
		internal(c, err)
		return
	}
	s.cookie(c, "vm_session", token, 7*86400, true)
	c.JSON(201, gin.H{"user": u})
}
func (s *Server) login(c *gin.Context) {
	v, ok := s.authInput(c)
	if !ok {
		return
	}
	var u User
	var hash string
	err := s.DB.QueryRowContext(c.Request.Context(), `SELECT id::text,email,password_hash FROM users WHERE email=$1`, v.Email).Scan(&u.ID, &u.Email, &hash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		internal(c, err)
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		hash = string(s.dummyHash)
	}
	valid := bcrypt.CompareHashAndPassword([]byte(hash), []byte(v.Password)) == nil
	if !valid || u.ID == "" {
		fail(c, 401, "LOGIN_FAILED", "邮箱或密码错误")
		return
	}
	token := randomToken()
	tx, err := s.DB.BeginTx(c.Request.Context(), nil)
	if err != nil {
		internal(c, err)
		return
	}
	defer tx.Rollback()
	if old, e := c.Cookie("vm_session"); e == nil {
		if _, err = tx.ExecContext(c.Request.Context(), `DELETE FROM sessions WHERE token_hash=$1`, digest(old)); err != nil {
			internal(c, err)
			return
		}
	}
	if _, err = tx.ExecContext(c.Request.Context(), `INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,$2,clock_timestamp()+interval '7 days')`, digest(token), u.ID); err != nil {
		internal(c, err)
		return
	}
	if err = tx.Commit(); err != nil {
		internal(c, err)
		return
	}
	s.cookie(c, "vm_session", token, 7*86400, true)
	c.JSON(200, gin.H{"user": u})
}
