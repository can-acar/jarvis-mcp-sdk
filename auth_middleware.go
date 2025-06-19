package mcp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash"
	"strings"
	"time"
)

// AuthConfig represents authentication configuration
type AuthConfig struct {
	Type     string                 `json:"type"`     // "bearer", "jwt", "api_key", "custom"
	Required bool                   `json:"required"` // Whether auth is required
	Config   map[string]interface{} `json:"config"`   // Auth-specific configuration
}

// JWTConfig represents JWT authentication configuration
type JWTConfig struct {
	Secret        string        `json:"secret"`
	Issuer        string        `json:"issuer"`
	Audience      string        `json:"audience"`
	ExpiryTime    time.Duration `json:"expiryTime"`
	RefreshTime   time.Duration `json:"refreshTime"`
	Algorithm     string        `json:"algorithm"` // HS256, HS384, HS512
	SkipClaimsExp bool          `json:"skipClaimsExp"`
}

// APIKeyConfig represents API key authentication configuration
type APIKeyConfig struct {
	Keys        []string `json:"keys"`        // Valid API keys
	HeaderName  string   `json:"headerName"`  // Header name (default: X-API-Key)
	QueryParam  string   `json:"queryParam"`  // Query parameter name
	AllowBoth   bool     `json:"allowBoth"`   // Allow both header and query param
}

// BearerTokenConfig represents bearer token authentication configuration
type BearerTokenConfig struct {
	Tokens []string `json:"tokens"` // Valid bearer tokens
}

// User represents an authenticated user
type User struct {
	ID       string                 `json:"id"`
	Username string                 `json:"username"`
	Email    string                 `json:"email"`
	Roles    []string               `json:"roles"`
	Claims   map[string]interface{} `json:"claims"`
}

// AuthResult represents authentication result
type AuthResult struct {
	Authenticated bool   `json:"authenticated"`
	User          *User  `json:"user"`
	Error         string `json:"error"`
	Token         string `json:"token"`
}

// AuthValidator is a function that validates authentication
type AuthValidator func(ctx context.Context, token string) (*AuthResult, error)

// BearerTokenMiddleware creates bearer token authentication middleware
func BearerTokenMiddleware(config BearerTokenConfig) MiddlewareFunc {
	return func(next RequestHandler) RequestHandler {
		return func(ctx context.Context, req *Request) *Response {
			// Extract token from request metadata or headers
			token := extractTokenFromRequest(req, "bearer")
			
			if token == "" {
				return createAuthErrorResponse(req.ID, "Missing bearer token")
			}
			
			// Validate token
			valid := false
			for _, validToken := range config.Tokens {
				if token == validToken {
					valid = true
					break
				}
			}
			
			if !valid {
				return createAuthErrorResponse(req.ID, "Invalid bearer token")
			}
			
			// Add user info to middleware context
			mctx := GetMiddlewareContext(ctx)
			mctx.Set("authenticated", true)
			mctx.Set("auth_type", "bearer")
			mctx.Set("token", token)
			
			return next(ctx, req)
		}
	}
}

// JWTMiddleware creates JWT authentication middleware
func JWTMiddleware(config JWTConfig) MiddlewareFunc {
	return func(next RequestHandler) RequestHandler {
		return func(ctx context.Context, req *Request) *Response {
			// Extract JWT token
			token := extractTokenFromRequest(req, "jwt")
			
			if token == "" {
				return createAuthErrorResponse(req.ID, "Missing JWT token")
			}
			
			// Validate JWT token
			claims, err := validateJWT(token, config)
			if err != nil {
				return createAuthErrorResponse(req.ID, fmt.Sprintf("Invalid JWT: %v", err))
			}
			
			// Create user from claims
			user := createUserFromClaims(claims)
			
			// Add user info to middleware context
			mctx := GetMiddlewareContext(ctx)
			mctx.Set("authenticated", true)
			mctx.Set("auth_type", "jwt")
			mctx.Set("user", user)
			mctx.Set("token", token)
			mctx.Set("claims", claims)
			
			return next(ctx, req)
		}
	}
}

// APIKeyMiddleware creates API key authentication middleware
func APIKeyMiddleware(config APIKeyConfig) MiddlewareFunc {
	return func(next RequestHandler) RequestHandler {
		return func(ctx context.Context, req *Request) *Response {
			// Extract API key
			apiKey := extractAPIKeyFromRequest(req, config)
			
			if apiKey == "" {
				return createAuthErrorResponse(req.ID, "Missing API key")
			}
			
			// Validate API key
			valid := false
			for _, validKey := range config.Keys {
				if apiKey == validKey {
					valid = true
					break
				}
			}
			
			if !valid {
				return createAuthErrorResponse(req.ID, "Invalid API key")
			}
			
			// Add auth info to middleware context
			mctx := GetMiddlewareContext(ctx)
			mctx.Set("authenticated", true)
			mctx.Set("auth_type", "api_key")
			mctx.Set("api_key", apiKey)
			
			return next(ctx, req)
		}
	}
}

// CustomAuthMiddleware creates custom authentication middleware
func CustomAuthMiddleware(validator AuthValidator) MiddlewareFunc {
	return func(next RequestHandler) RequestHandler {
		return func(ctx context.Context, req *Request) *Response {
			// Extract token (could be any format)
			token := extractTokenFromRequest(req, "custom")
			
			if token == "" {
				return createAuthErrorResponse(req.ID, "Missing authentication token")
			}
			
			// Use custom validator
			result, err := validator(ctx, token)
			if err != nil {
				return createAuthErrorResponse(req.ID, fmt.Sprintf("Authentication error: %v", err))
			}
			
			if !result.Authenticated {
				return createAuthErrorResponse(req.ID, result.Error)
			}
			
			// Add auth info to middleware context
			mctx := GetMiddlewareContext(ctx)
			mctx.Set("authenticated", true)
			mctx.Set("auth_type", "custom")
			mctx.Set("user", result.User)
			mctx.Set("token", token)
			
			return next(ctx, req)
		}
	}
}

// RequireAuthMiddleware ensures request is authenticated
func RequireAuthMiddleware() MiddlewareFunc {
	return func(next RequestHandler) RequestHandler {
		return func(ctx context.Context, req *Request) *Response {
			mctx := GetMiddlewareContext(ctx)
			
			authenticated, _ := mctx.Get("authenticated")
			if authenticated != true {
				return createAuthErrorResponse(req.ID, "Authentication required")
			}
			
			return next(ctx, req)
		}
	}
}

// RequireRoleMiddleware ensures user has required role
func RequireRoleMiddleware(requiredRole string) MiddlewareFunc {
	return func(next RequestHandler) RequestHandler {
		return func(ctx context.Context, req *Request) *Response {
			mctx := GetMiddlewareContext(ctx)
			
			userInterface, exists := mctx.Get("user")
			if !exists {
				return createAuthErrorResponse(req.ID, "User information not found")
			}
			
			user, ok := userInterface.(*User)
			if !ok {
				return createAuthErrorResponse(req.ID, "Invalid user information")
			}
			
			// Check if user has required role
			hasRole := false
			for _, role := range user.Roles {
				if role == requiredRole {
					hasRole = true
					break
				}
			}
			
			if !hasRole {
				return createAuthErrorResponse(req.ID, fmt.Sprintf("Role '%s' required", requiredRole))
			}
			
			return next(ctx, req)
		}
	}
}

// Helper functions

func extractTokenFromRequest(req *Request, authType string) string {
	// Try to extract from params (for HTTP requests, this would come from headers)
	if req.Params != nil {
		var params map[string]interface{}
		if err := json.Unmarshal(req.Params, &params); err == nil {
			// Check for authorization header
			if auth, exists := params["_auth"]; exists {
				if authStr, ok := auth.(string); ok {
					switch authType {
					case "bearer", "jwt":
						if strings.HasPrefix(authStr, "Bearer ") {
							return strings.TrimPrefix(authStr, "Bearer ")
						}
					case "custom":
						return authStr
					}
				}
			}
			
			// Check for specific token fields
			if token, exists := params["_token"]; exists {
				if tokenStr, ok := token.(string); ok {
					return tokenStr
				}
			}
		}
	}
	
	return ""
}

func extractAPIKeyFromRequest(req *Request, config APIKeyConfig) string {
	if req.Params != nil {
		var params map[string]interface{}
		if err := json.Unmarshal(req.Params, &params); err == nil {
			// Check for API key in headers simulation
			if headers, exists := params["_headers"]; exists {
				if headersMap, ok := headers.(map[string]interface{}); ok {
					headerName := config.HeaderName
					if headerName == "" {
						headerName = "X-API-Key"
					}
					
					if apiKey, exists := headersMap[headerName]; exists {
						if apiKeyStr, ok := apiKey.(string); ok {
							return apiKeyStr
						}
					}
				}
			}
			
			// Check for API key in query params simulation
			if query, exists := params["_query"]; exists {
				if queryMap, ok := query.(map[string]interface{}); ok {
					queryParam := config.QueryParam
					if queryParam == "" {
						queryParam = "api_key"
					}
					
					if apiKey, exists := queryMap[queryParam]; exists {
						if apiKeyStr, ok := apiKey.(string); ok {
							return apiKeyStr
						}
					}
				}
			}
		}
	}
	
	return ""
}

func createAuthErrorResponse(requestID interface{}, message string) *Response {
	return &Response{
		JSONRPC: "2.0",
		ID:      requestID,
		Error: &Error{
			Code:    401,
			Message: message,
		},
	}
}

// JWT validation functions (simplified implementation)

func validateJWT(tokenString string, config JWTConfig) (map[string]interface{}, error) {
	// Split token into parts
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT format")
	}
	
	header, payload, signature := parts[0], parts[1], parts[2]
	
	// Decode header
	headerBytes, err := base64.RawURLEncoding.DecodeString(header)
	if err != nil {
		return nil, fmt.Errorf("invalid header encoding")
	}
	
	var headerClaims map[string]interface{}
	if err := json.Unmarshal(headerBytes, &headerClaims); err != nil {
		return nil, fmt.Errorf("invalid header JSON")
	}
	
	// Check algorithm
	if alg, exists := headerClaims["alg"]; exists {
		if algStr, ok := alg.(string); ok {
			if config.Algorithm != "" && algStr != config.Algorithm {
				return nil, fmt.Errorf("unsupported algorithm: %s", algStr)
			}
		}
	}
	
	// Validate signature
	expectedSignature := generateSignature(header+"."+payload, config.Secret, config.Algorithm)
	if signature != expectedSignature {
		return nil, fmt.Errorf("invalid signature")
	}
	
	// Decode payload
	payloadBytes, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("invalid payload encoding")
	}
	
	var claims map[string]interface{}
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, fmt.Errorf("invalid payload JSON")
	}
	
	// Validate expiry
	if !config.SkipClaimsExp {
		if exp, exists := claims["exp"]; exists {
			if expFloat, ok := exp.(float64); ok {
				expiryTime := time.Unix(int64(expFloat), 0)
				if time.Now().After(expiryTime) {
					return nil, fmt.Errorf("token expired")
				}
			}
		}
	}
	
	// Validate issuer
	if config.Issuer != "" {
		if iss, exists := claims["iss"]; exists {
			if issStr, ok := iss.(string); ok {
				if issStr != config.Issuer {
					return nil, fmt.Errorf("invalid issuer")
				}
			}
		}
	}
	
	// Validate audience
	if config.Audience != "" {
		if aud, exists := claims["aud"]; exists {
			if audStr, ok := aud.(string); ok {
				if audStr != config.Audience {
					return nil, fmt.Errorf("invalid audience")
				}
			}
		}
	}
	
	return claims, nil
}

func generateSignature(data, secret, algorithm string) string {
	var h func() hash.Hash
	
	switch algorithm {
	case "HS512":
		h = sha512.New
	default: // HS256 (HS384 not available in this Go version)
		h = sha256.New
	}
	
	mac := hmac.New(h, []byte(secret))
	mac.Write([]byte(data))
	signature := mac.Sum(nil)
	
	return base64.RawURLEncoding.EncodeToString(signature)
}

func createUserFromClaims(claims map[string]interface{}) *User {
	user := &User{
		Claims: claims,
		Roles:  []string{},
	}
	
	// Extract standard claims
	if sub, exists := claims["sub"]; exists {
		if subStr, ok := sub.(string); ok {
			user.ID = subStr
		}
	}
	
	if name, exists := claims["name"]; exists {
		if nameStr, ok := name.(string); ok {
			user.Username = nameStr
		}
	}
	
	if email, exists := claims["email"]; exists {
		if emailStr, ok := email.(string); ok {
			user.Email = emailStr
		}
	}
	
	// Extract roles
	if roles, exists := claims["roles"]; exists {
		switch rolesValue := roles.(type) {
		case []string:
			user.Roles = rolesValue
		case []interface{}:
			for _, role := range rolesValue {
				if roleStr, ok := role.(string); ok {
					user.Roles = append(user.Roles, roleStr)
				}
			}
		case string:
			user.Roles = strings.Split(rolesValue, ",")
		}
	}
	
	return user
}

// Auth helper functions for server

// GetCurrentUser returns the current authenticated user from context
func GetCurrentUser(ctx context.Context) *User {
	mctx := GetMiddlewareContext(ctx)
	if userInterface, exists := mctx.Get("user"); exists {
		if user, ok := userInterface.(*User); ok {
			return user
		}
	}
	return nil
}

// IsAuthenticated checks if the request is authenticated
func IsAuthenticated(ctx context.Context) bool {
	mctx := GetMiddlewareContext(ctx)
	authenticated, _ := mctx.Get("authenticated")
	return authenticated == true
}

// GetAuthType returns the authentication type used
func GetAuthType(ctx context.Context) string {
	mctx := GetMiddlewareContext(ctx)
	return mctx.GetString("auth_type")
}