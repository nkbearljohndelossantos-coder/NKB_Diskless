package auth_rbac

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"nkb-diskless/internal/database"
	"nkb-diskless/internal/models"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const (
	UserContextKey   contextKey = "nkb_user"
	ClientContextKey contextKey = "nkb_client"
)

// Claims represents JWT payload for NKB administrators.
type Claims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

type AuthService struct {
	jwtSecret []byte
	db        *database.DB
	expiry    time.Duration
}

func NewAuthService(jwtSecret string, db *database.DB, expiryHours int) *AuthService {
	if expiryHours <= 0 {
		expiryHours = 24
	}
	return &AuthService{
		jwtSecret: []byte(jwtSecret),
		db:        db,
		expiry:    time.Duration(expiryHours) * time.Hour,
	}
}

// GenerateToken creates a signed JWT for an authenticated user.
func (s *AuthService) GenerateToken(user *models.User) (string, time.Time, error) {
	expiresAt := time.Now().Add(s.expiry)
	claims := Claims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "nkb-diskless",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString(s.jwtSecret)
	return tokenStr, expiresAt, err
}

// ValidateToken parses and verifies a JWT string.
func (s *AuthService) ValidateToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.jwtSecret, nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("invalid token")
}

// validateCSRF ensures cross-origin state-changing requests cannot be forged via session cookies.
func validateCSRF(r *http.Request) bool {
	// Block explicit cross-site fetch metadata
	fetchSite := r.Header.Get("Sec-Fetch-Site")
	if fetchSite == "cross-site" {
		return false
	}
	if fetchSite == "same-origin" || fetchSite == "same-site" {
		return true
	}

	origin := r.Header.Get("Origin")
	if origin != "" {
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		// Match against r.Host
		if strings.EqualFold(u.Host, r.Host) {
			return true
		}
		// Allow local dev frontend origins
		uHost := u.Hostname()
		rHost := r.Host
		if strings.Contains(rHost, ":") {
			rHost = strings.Split(rHost, ":")[0]
		}
		isULocal := uHost == "localhost" || uHost == "127.0.0.1" || uHost == "::1"
		isRLocal := rHost == "localhost" || rHost == "127.0.0.1" || rHost == "::1"
		if isULocal && isRLocal {
			uPort := u.Port()
			if uPort == "5173" || uPort == "3000" {
				return true
			}
		}
		return false
	}

	referer := r.Header.Get("Referer")
	if referer != "" {
		u, err := url.Parse(referer)
		if err != nil {
			return false
		}
		if strings.EqualFold(u.Host, r.Host) {
			return true
		}
		uHost := u.Hostname()
		rHost := r.Host
		if strings.Contains(rHost, ":") {
			rHost = strings.Split(rHost, ":")[0]
		}
		isULocal := uHost == "localhost" || uHost == "127.0.0.1" || uHost == "::1"
		isRLocal := rHost == "localhost" || rHost == "127.0.0.1" || rHost == "::1"
		if isULocal && isRLocal {
			uPort := u.Port()
			if uPort == "5173" || uPort == "3000" {
				return true
			}
		}
		return false
	}

	// Fail closed if state-changing cookie request lacks Origin, Referer, and Sec-Fetch-Site
	return false
}

// RequireAuth middleware verifies administrator JWT from Authorization: Bearer <token> or secure cookie.
func (s *AuthService) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var tokenStr string
		var isCookieAuth bool

		authHeader := r.Header.Get("Authorization")
		if authHeader != "" {
			parts := strings.Split(authHeader, " ")
			if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
				tokenStr = parts[1]
			}
		}

		if tokenStr == "" {
			if cookie, err := r.Cookie("nkb_session"); err == nil && cookie.Value != "" {
				tokenStr = cookie.Value
				isCookieAuth = true
			}
		}

		if tokenStr == "" {
			http.Error(w, `{"error":"authorization header or session cookie required"}`, http.StatusUnauthorized)
			return
		}

		// CSRF Protection for state-changing operations authenticated via cookie
		if isCookieAuth && (r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete || r.Method == http.MethodPatch) {
			if !validateCSRF(r) {
				http.Error(w, `{"error":"forbidden: cross-site request forgery protection rejected request"}`, http.StatusForbidden)
				return
			}
		}

		claims, err := s.ValidateToken(tokenStr)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"unauthorized: %s"}`, err.Error()), http.StatusUnauthorized)
			return
		}

		user, err := s.db.GetUserByID(claims.UserID)
		if err != nil || user == nil || !user.IsActive {
			http.Error(w, `{"error":"user not found or inactive"}`, http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), UserContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// RequireRole checks that the authenticated user possesses one of the allowed roles.
func (s *AuthService) RequireRole(roles ...string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return s.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
			user, ok := r.Context().Value(UserContextKey).(*models.User)
			if !ok || user == nil {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			allowed := false
			for _, role := range roles {
				if strings.EqualFold(user.Role, role) || user.Role == "superadmin" {
					allowed = true
					break
				}
			}

			if !allowed {
				http.Error(w, `{"error":"forbidden: insufficient role permissions"}`, http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireAgentToken middleware authenticates workstation agents using the X-Device-Token header.
func (s *AuthService) RequireAgentToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-Device-Token")
		if token == "" {
			// Fall back to query param for websockets
			token = r.URL.Query().Get("device_token")
		}

		if token == "" {
			http.Error(w, `{"error":"X-Device-Token header required"}`, http.StatusUnauthorized)
			return
		}

		client, err := s.db.AuthenticateAgent(token)
		if err != nil || client == nil {
			http.Error(w, `{"error":"invalid or revoked device token"}`, http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), ClientContextKey, client)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}
