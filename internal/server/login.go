package server

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func (s *Server) showLogin(c *gin.Context) {
	next := validateRedirectURL(c.DefaultQuery("next", "/"))
	data := s.addVersionToTemplateData(gin.H{"next": next})
	if errMsg := c.Query("error"); errMsg != "" {
		data["error"] = errMsg
	}

	c.Status(http.StatusOK)
	if err := s.loginTmpl.ExecuteTemplate(c.Writer, "base", data); err != nil {
		http.Error(c.Writer, "failed to render login page", http.StatusInternalServerError)
	}
}

func (s *Server) doLogin(c *gin.Context) {
	var username, password, next string
	contentType := c.GetHeader("Content-Type")

	if strings.Contains(contentType, "application/json") {
		var jsonData struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Next     string `json:"next"`
		}
		if err := c.ShouldBindJSON(&jsonData); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request format"})
			return
		}
		username, password, next = jsonData.Username, jsonData.Password, jsonData.Next
	} else {
		username, password, next = c.PostForm("username"), c.PostForm("password"), c.PostForm("next")
	}

	next = validateRedirectURL(next)

	if !s.validateCredentials(username, password) {
		if strings.Contains(c.GetHeader("Accept"), "application/json") {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
			return
		}
		c.Status(http.StatusOK)
		if err := s.loginTmpl.ExecuteTemplate(c.Writer, "base", gin.H{"error": "Invalid username or password", "next": next}); err != nil {
			http.Error(c.Writer, "failed to render login page", http.StatusInternalServerError)
		}
		return
	}

	token := s.sessionStore.NewToken()
	s.sessionStore.Add(token)

	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("slimserve_session", token, 0, "/", "", c.Request.TLS != nil, true)

	if strings.Contains(contentType, "application/json") {
		c.JSON(http.StatusOK, gin.H{"success": true, "redirect": next})
	} else {
		c.Redirect(http.StatusFound, next)
	}
}

func (s *Server) validateCredentials(username, password string) bool {
	if !s.config.EnableAuth || s.config.Username == "" {
		return false
	}

	if subtle.ConstantTimeCompare([]byte(username), []byte(s.config.Username)) != 1 {
		return false
	}

	if s.config.PasswordHash != "" {
		return VerifyPassword(s.config.PasswordHash, password)
	}

	if s.config.Password == "" {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(password), []byte(s.config.Password)) == 1
}

func validateRedirectURL(next string) string {
	if next == "" {
		return "/"
	}
	if !strings.HasPrefix(next, "/") || strings.Contains(next, "://") || strings.HasPrefix(next, "//") {
		return "/"
	}
	return next
}
