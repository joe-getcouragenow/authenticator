package deviceapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-kit/kit/log"
	"github.com/google/go-cmp/cmp"
	"github.com/gorilla/mux"

	auth "github.com/fmitra/authenticator"
	"github.com/fmitra/authenticator/internal/httpapi"
	"github.com/fmitra/authenticator/internal/postgres"
	"github.com/fmitra/authenticator/internal/test"
)

func TestDeviceAPI_Create(t *testing.T) {
	tt := []struct {
		name            string
		statusCode      int
		authHeader      bool
		errMessage      string
		tokenValidateFn func() (*auth.Token, error)
		userFn          func() (*auth.User, error)
		webauthnFn      func() ([]byte, error)
	}{
		{
			name:       "Authentication error with no token",
			statusCode: http.StatusUnauthorized,
			authHeader: false,
			errMessage: "User is not authenticated",
			tokenValidateFn: func() (*auth.Token, error) {
				return &auth.Token{UserID: "user-id", State: auth.JWTAuthorized}, nil
			},
			userFn: func() (*auth.User, error) {
				return &auth.User{}, nil
			},
			webauthnFn: func() ([]byte, error) {
				return []byte(`{"result":"challenge"}`), nil
			},
		},
		{
			name:       "Authentication error with bad token",
			statusCode: http.StatusUnauthorized,
			authHeader: true,
			errMessage: "Bad token",
			tokenValidateFn: func() (*auth.Token, error) {
				return nil, auth.ErrInvalidToken("bad token")
			},
			userFn: func() (*auth.User, error) {
				return &auth.User{}, nil
			},
			webauthnFn: func() ([]byte, error) {
				return []byte(`{"result":"challenge"}`), nil
			},
		},
		{
			name:       "User query error",
			statusCode: http.StatusBadRequest,
			authHeader: true,
			errMessage: "No user found",
			tokenValidateFn: func() (*auth.Token, error) {
				return &auth.Token{UserID: "user-id", State: auth.JWTAuthorized}, nil
			},
			userFn: func() (*auth.User, error) {
				return nil, auth.ErrBadRequest("no user found")
			},
			webauthnFn: func() ([]byte, error) {
				return []byte("challenge"), nil
			},
		},
		{
			name:       "Non domain error",
			statusCode: http.StatusInternalServerError,
			authHeader: true,
			errMessage: "An internal error occurred",
			tokenValidateFn: func() (*auth.Token, error) {
				return &auth.Token{UserID: "user-id", State: auth.JWTAuthorized}, nil
			},
			userFn: func() (*auth.User, error) {
				return nil, fmt.Errorf("whoops")
			},
			webauthnFn: func() ([]byte, error) {
				return []byte(`{"result":"challenge"}`), nil
			},
		},
		{
			name:       "Successful request",
			statusCode: http.StatusOK,
			authHeader: true,
			errMessage: "",
			tokenValidateFn: func() (*auth.Token, error) {
				return &auth.Token{UserID: "user-id", State: auth.JWTAuthorized}, nil
			},
			userFn: func() (*auth.User, error) {
				return &auth.User{}, nil
			},
			webauthnFn: func() ([]byte, error) {
				return []byte(`{"result":"challenge"}`), nil
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			router := mux.NewRouter()
			webauthnSvc := &test.WebAuthnService{
				BeginSignUpFn: tc.webauthnFn,
			}
			repoMngr := &test.RepositoryManager{
				UserFn: func() auth.UserRepository {
					return &test.UserRepository{
						ByIdentityFn: tc.userFn,
					}
				},
			}
			tokenSvc := &test.TokenService{
				ValidateFn: tc.tokenValidateFn,
			}
			svc := NewService(
				WithLogger(&test.Logger{}),
				WithWebAuthn(webauthnSvc),
				WithRepoManager(repoMngr),
			)

			req, err := http.NewRequest("POST", "/api/v1/device", nil)
			if err != nil {
				t.Fatal("failed to create request:", err)
			}

			if tc.authHeader {
				test.SetAuthHeaders(req)
			}

			logger := log.NewJSONLogger(log.NewSyncWriter(os.Stderr))
			SetupHTTPHandler(svc, router, tokenSvc, logger, &httpapi.MockLimiterFactory{})

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			if rr.Code != tc.statusCode {
				t.Errorf("incorrect status code, want %v got %v", tc.statusCode, rr.Code)
			}

			err = test.ValidateErrMessage(tc.errMessage, rr.Body)
			if err != nil {
				t.Error(err)
			}
		})
	}
}

func TestDeviceAPI_Verify(t *testing.T) {
	tt := []struct {
		name            string
		statusCode      int
		authHeader      bool
		errMessage      string
		tokenValidateFn func() (*auth.Token, error)
		tokenCreateFn   func() (*auth.Token, error)
		tokenSignFn     func() (string, error)
		userFn          func() (*auth.User, error)
		webauthnFn      func() (*auth.Device, error)
	}{
		{
			name:       "Authentication error with no token",
			statusCode: http.StatusUnauthorized,
			authHeader: false,
			errMessage: "User is not authenticated",
			tokenCreateFn: func() (*auth.Token, error) {
				return &auth.Token{CodeHash: "token:1:address:phone"}, nil
			},
			tokenSignFn: func() (string, error) {
				return "token", nil
			},
			tokenValidateFn: func() (*auth.Token, error) {
				return &auth.Token{UserID: "user-id", State: auth.JWTAuthorized}, nil
			},
			userFn: func() (*auth.User, error) {
				return &auth.User{}, nil
			},
			webauthnFn: func() (*auth.Device, error) {
				return &auth.Device{}, nil
			},
		},
		{
			name:       "Authentication error with bad token",
			statusCode: http.StatusUnauthorized,
			authHeader: true,
			errMessage: "Bad token",
			tokenCreateFn: func() (*auth.Token, error) {
				return &auth.Token{CodeHash: "token:1:address:phone"}, nil
			},
			tokenSignFn: func() (string, error) {
				return "token", nil
			},
			tokenValidateFn: func() (*auth.Token, error) {
				return nil, auth.ErrInvalidToken("bad token")
			},
			userFn: func() (*auth.User, error) {
				return &auth.User{}, nil
			},
			webauthnFn: func() (*auth.Device, error) {
				return &auth.Device{}, nil
			},
		},
		{
			name:       "User query error",
			statusCode: http.StatusBadRequest,
			authHeader: true,
			errMessage: "No user found",
			tokenValidateFn: func() (*auth.Token, error) {
				return &auth.Token{UserID: "user-id", State: auth.JWTAuthorized}, nil
			},
			userFn: func() (*auth.User, error) {
				return nil, auth.ErrBadRequest("no user found")
			},
			webauthnFn: func() (*auth.Device, error) {
				return &auth.Device{}, nil
			},
		},
		{
			name:       "Webauthn signup error",
			statusCode: http.StatusInternalServerError,
			authHeader: true,
			errMessage: "An internal error occurred",
			tokenCreateFn: func() (*auth.Token, error) {
				return &auth.Token{CodeHash: "token:1:address:phone"}, nil
			},
			tokenSignFn: func() (string, error) {
				return "token", nil
			},
			tokenValidateFn: func() (*auth.Token, error) {
				return &auth.Token{UserID: "user-id", State: auth.JWTAuthorized}, nil
			},
			userFn: func() (*auth.User, error) {
				return &auth.User{}, nil
			},
			webauthnFn: func() (*auth.Device, error) {
				return nil, fmt.Errorf("whoops")
			},
		},
		{
			name:       "Successful request",
			statusCode: http.StatusCreated,
			authHeader: true,
			errMessage: "",
			tokenCreateFn: func() (*auth.Token, error) {
				return &auth.Token{CodeHash: "token:1:address:phone"}, nil
			},
			tokenSignFn: func() (string, error) {
				return "token", nil
			},
			tokenValidateFn: func() (*auth.Token, error) {
				return &auth.Token{UserID: "user-id", State: auth.JWTAuthorized}, nil
			},
			userFn: func() (*auth.User, error) {
				return &auth.User{}, nil
			},
			webauthnFn: func() (*auth.Device, error) {
				return &auth.Device{}, nil
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			router := mux.NewRouter()
			webauthnSvc := &test.WebAuthnService{
				FinishSignUpFn: tc.webauthnFn,
			}
			repoMngr := &test.RepositoryManager{
				UserFn: func() auth.UserRepository {
					return &test.UserRepository{
						ByIdentityFn: tc.userFn,
					}
				},
			}
			tokenSvc := &test.TokenService{
				ValidateFn: tc.tokenValidateFn,
				CreateFn:   tc.tokenCreateFn,
				SignFn:     tc.tokenSignFn,
			}
			svc := NewService(
				WithLogger(&test.Logger{}),
				WithWebAuthn(webauthnSvc),
				WithRepoManager(repoMngr),
				WithTokenService(tokenSvc),
			)

			req, err := http.NewRequest("POST", "/api/v1/device/verify", nil)
			if err != nil {
				t.Fatal("failed to create request:", err)
			}

			if tc.authHeader {
				test.SetAuthHeaders(req)
			}

			logger := log.NewJSONLogger(log.NewSyncWriter(os.Stderr))
			SetupHTTPHandler(svc, router, tokenSvc, logger, &httpapi.MockLimiterFactory{})

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			if rr.Code != tc.statusCode {
				t.Errorf("incorrect status code, want %v got %v", tc.statusCode, rr.Code)
			}

			err = test.ValidateErrMessage(tc.errMessage, rr.Body)
			if err != nil {
				t.Error(err)
			}
		})
	}
}

func TestDeviceAPI_Remove(t *testing.T) {
	tt := []struct {
		user            auth.User
		devices         []*auth.Device
		name            string
		errMessage      string
		devicePath      string
		statusCode      int
		totalDevices    int
		tokenValidateFn func() (*auth.Token, error)
		tokenCreateFn   func() (*auth.Token, error)
		tokenSignFn     func() (string, error)
		authHeader      bool
		isDeviceAllowed bool
	}{
		{
			name:       "Authentication error with no token",
			statusCode: http.StatusUnauthorized,
			authHeader: false,
			errMessage: "User is not authenticated",
			tokenCreateFn: func() (*auth.Token, error) {
				return &auth.Token{CodeHash: "token:1:address:phone"}, nil
			},
			tokenSignFn: func() (string, error) {
				return "token", nil
			},
			tokenValidateFn: func() (*auth.Token, error) {
				return &auth.Token{UserID: "user-id", State: auth.JWTAuthorized}, nil
			},
			user: auth.User{
				Password: "swordfish",
				Email: sql.NullString{
					String: "jane@example.com",
					Valid:  true,
				},
				IsVerified:      true,
				IsDeviceAllowed: true,
			},
			totalDevices:    1,
			devicePath:      "/api/v1/device/%s",
			isDeviceAllowed: true,
			devices: []*auth.Device{
				{
					ClientID:  []byte(""),
					PublicKey: []byte(""),
					AAGUID:    []byte(""),
					SignCount: 0,
				},
			},
		},
		{
			name:       "IsDeviceAllowed is true after removal",
			statusCode: http.StatusOK,
			authHeader: true,
			errMessage: "",
			tokenCreateFn: func() (*auth.Token, error) {
				return &auth.Token{CodeHash: "token:1:address:phone"}, nil
			},
			tokenSignFn: func() (string, error) {
				return "token", nil
			},
			tokenValidateFn: func() (*auth.Token, error) {
				return &auth.Token{UserID: "user-id", State: auth.JWTAuthorized}, nil
			},
			user: auth.User{
				Password: "swordfish",
				Email: sql.NullString{
					String: "jane@example.com",
					Valid:  true,
				},
				IsDeviceAllowed: true,
				IsVerified:      true,
			},
			totalDevices:    1,
			devicePath:      "/api/v1/device/%s",
			isDeviceAllowed: true,
			devices: []*auth.Device{
				{
					ClientID:  []byte(""),
					PublicKey: []byte(""),
					AAGUID:    []byte(""),
					SignCount: 0,
				},
				{
					ClientID:  []byte(""),
					PublicKey: []byte(""),
					AAGUID:    []byte(""),
					SignCount: 0,
				},
			},
		},
		{
			name:       "Device not found",
			statusCode: http.StatusBadRequest,
			authHeader: true,
			errMessage: "Device does not exist",
			tokenCreateFn: func() (*auth.Token, error) {
				return &auth.Token{CodeHash: "token:1:address:phone"}, nil
			},
			tokenSignFn: func() (string, error) {
				return "token", nil
			},
			tokenValidateFn: func() (*auth.Token, error) {
				return &auth.Token{UserID: "user-id", State: auth.JWTAuthorized}, nil
			},
			user: auth.User{
				Password: "swordfish",
				Email: sql.NullString{
					String: "jane@example.com",
					Valid:  true,
				},
				IsDeviceAllowed: true,
				IsVerified:      true,
			},
			totalDevices:    1,
			devicePath:      "/api/v1/device/does-not-exist%s",
			isDeviceAllowed: true,
			devices: []*auth.Device{
				{
					ClientID:  []byte(""),
					PublicKey: []byte(""),
					AAGUID:    []byte(""),
					SignCount: 0,
				},
			},
		},
		{
			name:       "IsDeviceAllowed is false after removal",
			statusCode: http.StatusOK,
			authHeader: true,
			errMessage: "",
			tokenCreateFn: func() (*auth.Token, error) {
				return &auth.Token{CodeHash: "token:1:address:phone"}, nil
			},
			tokenSignFn: func() (string, error) {
				return "token", nil
			},
			tokenValidateFn: func() (*auth.Token, error) {
				return &auth.Token{UserID: "user-id", State: auth.JWTAuthorized}, nil
			},
			user: auth.User{
				Password: "swordfish",
				Email: sql.NullString{
					String: "jane@example.com",
					Valid:  true,
				},
				IsDeviceAllowed: true,
				IsVerified:      true,
			},
			totalDevices:    0,
			devicePath:      "/api/v1/device/%s",
			isDeviceAllowed: false,
			devices: []*auth.Device{
				{
					ClientID:  []byte(""),
					PublicKey: []byte(""),
					AAGUID:    []byte(""),
					SignCount: 0,
				},
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			logger := log.NewJSONLogger(log.NewSyncWriter(os.Stderr))
			pgDB, err := test.NewPGDB()
			if err != nil {
				t.Fatal("failed to create test database:", err)
			}
			defer pgDB.DropDB()

			repoMngr := postgres.TestClient(pgDB.DB)
			err = repoMngr.User().Create(ctx, &tc.user)
			if err != nil {
				t.Fatal("failed to create user:", err)
			}

			for _, device := range tc.devices {
				device.UserID = tc.user.ID
				if err = repoMngr.Device().Create(ctx, device); err != nil {
					t.Fatal("failed to create device:", err)
				}
			}

			router := mux.NewRouter()
			webauthnSvc := &test.WebAuthnService{}
			tokenSvc := &test.TokenService{
				ValidateFn: func() (*auth.Token, error) {
					return &auth.Token{
						UserID: tc.user.ID,
						State:  auth.JWTAuthorized,
					}, nil
				},
				CreateFn: tc.tokenCreateFn,
				SignFn:   tc.tokenSignFn,
			}
			svc := NewService(
				WithLogger(&test.Logger{}),
				WithWebAuthn(webauthnSvc),
				WithRepoManager(repoMngr),
				WithTokenService(tokenSvc),
			)

			deviceID := tc.devices[0].ID
			req, err := http.NewRequest(
				"DELETE",
				fmt.Sprintf(tc.devicePath, deviceID),
				nil,
			)
			if err != nil {
				t.Fatal("failed to create request:", err)
			}

			if tc.authHeader {
				test.SetAuthHeaders(req)
			}

			SetupHTTPHandler(svc, router, tokenSvc, logger, &httpapi.MockLimiterFactory{})

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			if rr.Code != tc.statusCode {
				t.Errorf("incorrect status code, want %v got %v", tc.statusCode, rr.Code)
			}

			err = test.ValidateErrMessage(tc.errMessage, rr.Body)
			if err != nil {
				t.Error(err)
			}

			user, err := repoMngr.User().ByIdentity(ctx, "ID", tc.user.ID)
			if err != nil {
				t.Error("failed to get user:", err)
			}

			devices, err := repoMngr.Device().ByUserID(ctx, tc.user.ID)
			if err != nil {
				t.Error("failed to check devices:", err)
			}

			if !cmp.Equal(tc.totalDevices, len(devices)) {
				t.Error("total remaining devices does not match", cmp.Diff(
					tc.totalDevices,
					len(devices),
				))
			}

			if !cmp.Equal(tc.isDeviceAllowed, user.IsDeviceAllowed) {
				t.Error("IsDeviceAllowed does not match", cmp.Diff(
					tc.isDeviceAllowed,
					user.IsDeviceAllowed,
				))
			}
		})
	}
}

func TestDeviceAPI_List(t *testing.T) {
	tt := []struct {
		name            string
		statusCode      int
		authHeader      bool
		errMessage      string
		tokenValidateFn func() (*auth.Token, error)
		deviceFn        func() ([]*auth.Device, error)
	}{
		{
			name:       "Authentication error with no token",
			statusCode: http.StatusUnauthorized,
			authHeader: false,
			errMessage: "User is not authenticated",
			tokenValidateFn: func() (*auth.Token, error) {
				return &auth.Token{UserID: "user-id", State: auth.JWTAuthorized}, nil
			},
			deviceFn: func() ([]*auth.Device, error) {
				return []*auth.Device{
					{},
				}, nil
			},
		},
		{
			name:       "Authentication error with bad token",
			statusCode: http.StatusUnauthorized,
			authHeader: true,
			errMessage: "Bad token",
			tokenValidateFn: func() (*auth.Token, error) {
				return nil, auth.ErrInvalidToken("bad token")
			},
			deviceFn: func() ([]*auth.Device, error) {
				return []*auth.Device{
					{},
				}, nil
			},
		},
		{
			name:       "Device query error",
			statusCode: http.StatusInternalServerError,
			authHeader: true,
			errMessage: "An internal error occurred",
			tokenValidateFn: func() (*auth.Token, error) {
				return &auth.Token{UserID: "user-id", State: auth.JWTAuthorized}, nil
			},
			deviceFn: func() ([]*auth.Device, error) {
				return nil, fmt.Errorf("whoops")
			},
		},
		{
			name:       "Successful request",
			statusCode: http.StatusOK,
			authHeader: true,
			errMessage: "",
			tokenValidateFn: func() (*auth.Token, error) {
				return &auth.Token{UserID: "user-id", State: auth.JWTAuthorized}, nil
			},
			deviceFn: func() ([]*auth.Device, error) {
				return []*auth.Device{
					{},
				}, nil
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			router := mux.NewRouter()
			webauthnSvc := &test.WebAuthnService{}
			repoMngr := &test.RepositoryManager{
				DeviceFn: func() auth.DeviceRepository {
					return &test.DeviceRepository{
						ByUserIDFn: tc.deviceFn,
					}
				},
			}
			tokenSvc := &test.TokenService{
				ValidateFn: tc.tokenValidateFn,
			}
			svc := NewService(
				WithLogger(&test.Logger{}),
				WithWebAuthn(webauthnSvc),
				WithRepoManager(repoMngr),
			)

			req, err := http.NewRequest("GET", "/api/v1/device", nil)
			if err != nil {
				t.Fatal("failed to create request:", err)
			}

			if tc.authHeader {
				test.SetAuthHeaders(req)
			}

			logger := log.NewJSONLogger(log.NewSyncWriter(os.Stderr))
			SetupHTTPHandler(svc, router, tokenSvc, logger, &httpapi.MockLimiterFactory{})

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			if rr.Code != tc.statusCode {
				t.Errorf("incorrect status code, want %v got %v", tc.statusCode, rr.Code)
			}

			err = test.ValidateErrMessage(tc.errMessage, rr.Body)
			if err != nil {
				t.Error(err)
			}

			if tc.errMessage != "" {
				return
			}

			var resp listResponse
			err = json.NewDecoder(rr.Body).Decode(&resp)
			if err != nil {
				t.Fatal("cannot decode response", err)
			}

			if len(resp.Devices) != 1 {
				t.Errorf("incorrect devices returned, want 1, got %v", len(resp.Devices))
			}
		})
	}
}

func TestDeviceAPI_Rename(t *testing.T) {
	tt := []struct {
		user            auth.User
		devices         []*auth.Device
		name            string
		errMessage      string
		devicePath      string
		statusCode      int
		tokenValidateFn func() (*auth.Token, error)
		deviceName      string
		authHeader      bool
	}{
		{
			name:       "Authentication error with no token",
			statusCode: http.StatusUnauthorized,
			authHeader: false,
			errMessage: "User is not authenticated",
			deviceName: "Device",
			tokenValidateFn: func() (*auth.Token, error) {
				return &auth.Token{UserID: "user-id", State: auth.JWTAuthorized}, nil
			},
			user: auth.User{
				Password: "swordfish",
				Email: sql.NullString{
					String: "jane@example.com",
					Valid:  true,
				},
				IsVerified:      true,
				IsDeviceAllowed: true,
			},
			devicePath: "/api/v1/device/%s",
		},
		{
			name:       "Device name is changed",
			statusCode: http.StatusOK,
			authHeader: true,
			errMessage: "",
			deviceName: "Device 2",
			tokenValidateFn: func() (*auth.Token, error) {
				return &auth.Token{UserID: "user-id", State: auth.JWTAuthorized}, nil
			},
			user: auth.User{
				Password: "swordfish",
				Email: sql.NullString{
					String: "jane@example.com",
					Valid:  true,
				},
				IsDeviceAllowed: true,
				IsVerified:      true,
			},
			devicePath: "/api/v1/device/%s",
		},
		{
			name:       "Device not found",
			statusCode: http.StatusBadRequest,
			authHeader: true,
			errMessage: "Device does not exist",
			deviceName: "Device",
			tokenValidateFn: func() (*auth.Token, error) {
				return &auth.Token{UserID: "user-id", State: auth.JWTAuthorized}, nil
			},
			user: auth.User{
				Password: "swordfish",
				Email: sql.NullString{
					String: "jane@example.com",
					Valid:  true,
				},
				IsDeviceAllowed: true,
				IsVerified:      true,
			},
			devicePath: "/api/v1/device/does-not-exist%s",
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			logger := log.NewJSONLogger(log.NewSyncWriter(os.Stderr))
			pgDB, err := test.NewPGDB()
			if err != nil {
				t.Fatal("failed to create test database:", err)
			}
			defer pgDB.DropDB()

			repoMngr := postgres.TestClient(pgDB.DB)
			err = repoMngr.User().Create(ctx, &tc.user)
			if err != nil {
				t.Fatal("failed to create user:", err)
			}

			device := &auth.Device{
				UserID:    tc.user.ID,
				ClientID:  []byte(""),
				PublicKey: []byte(""),
				AAGUID:    []byte(""),
				SignCount: 0,
				Name:      "Device",
			}
			if err = repoMngr.Device().Create(ctx, device); err != nil {
				t.Fatal("failed to create device:", err)
			}

			router := mux.NewRouter()
			webauthnSvc := &test.WebAuthnService{}
			tokenSvc := &test.TokenService{
				ValidateFn: func() (*auth.Token, error) {
					return &auth.Token{
						UserID: tc.user.ID,
						State:  auth.JWTAuthorized,
					}, nil
				},
			}
			svc := NewService(
				WithLogger(&test.Logger{}),
				WithWebAuthn(webauthnSvc),
				WithRepoManager(repoMngr),
				WithTokenService(tokenSvc),
			)

			request := bytes.NewBuffer([]byte(
				fmt.Sprintf(`{"name": "%s"}`, tc.deviceName),
			))
			req, err := http.NewRequest(
				"PATCH",
				fmt.Sprintf(tc.devicePath, device.ID),
				request,
			)
			if err != nil {
				t.Fatal("failed to create request:", err)
			}

			if tc.authHeader {
				test.SetAuthHeaders(req)
			}

			SetupHTTPHandler(svc, router, tokenSvc, logger, &httpapi.MockLimiterFactory{})

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			if rr.Code != tc.statusCode {
				t.Errorf("incorrect status code, want %v got %v", tc.statusCode, rr.Code)
			}

			err = test.ValidateErrMessage(tc.errMessage, rr.Body)
			if err != nil {
				t.Error(err)
			}

			device, err = repoMngr.Device().ByID(ctx, device.ID)
			if err != nil {
				t.Error("failed to check device:", err)
			}

			if !cmp.Equal(tc.deviceName, device.Name) {
				t.Error("device name does not match", cmp.Diff(
					tc.deviceName, device.Name,
				))
			}
		})
	}
}
