package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

const (
	pbkdf2Iterations = 600000
	pbkdf2KeyLength  = 32
	saltBytes        = 16
	tokenBytes       = 32
)

func HashPassword(password string) (string, error) {
	if password == "" {
		return "", errors.New("password must not be empty")
	}
	salt := make([]byte, saltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, pbkdf2KeyLength)
	if err != nil {
		return "", err
	}
	return "pbkdf2-sha256$" + strconv.Itoa(pbkdf2Iterations) + "$" +
		hex.EncodeToString(salt) + "$" + hex.EncodeToString(key), nil
}

func CheckPassword(encoded, password string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false, errors.New("unsupported password hash format")
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter <= 0 {
		return false, errors.New("invalid password hash iteration count")
	}
	salt, err := hex.DecodeString(parts[2])
	if err != nil {
		return false, errors.New("invalid password hash salt")
	}
	want, err := hex.DecodeString(parts[3])
	if err != nil || len(want) == 0 {
		return false, errors.New("invalid password hash digest")
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

func NewToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

type Role string

const (
	RoleViewer   Role = "viewer"
	RoleOperator Role = "operator"
	RoleQC       Role = "qc"
	RoleNPI      Role = "npi"
	RoleAdmin    Role = "admin"
)

var ValidRoles = []Role{RoleViewer, RoleOperator, RoleQC, RoleNPI, RoleAdmin}

func IsValidRole(r Role) bool {
	for _, v := range ValidRoles {
		if v == r {
			return true
		}
	}
	return false
}

type Permission string

const (
	PermOrderList       Permission = "orders:list"
	PermOrderCreate     Permission = "orders:create"
	PermOrderView       Permission = "orders:view"
	PermOrderTransition Permission = "orders:transition"
	PermHoldCreate      Permission = "holds:create"
	PermHoldResolve     Permission = "holds:resolve"
	PermQCSubmit        Permission = "qc:submit"
	PermAuditView       Permission = "audit:view"
	PermUsersManage     Permission = "users:manage"
	PermScanUse         Permission = "scan:use"
	PermPickUse         Permission = "pick:use"
	PermCommentRead     Permission = "comments:read"
	PermCommentPost     Permission = "comments:post"
	PermNotifyRead      Permission = "notifications:read"
	PermManualManage    Permission = "manuals:manage"
	PermSIList          Permission = "si:list"
	PermSIView           Permission = "si:view"
	PermSICreate         Permission = "si:create"
	PermSIUpdate         Permission = "si:update"
	PermSITransition     Permission = "si:transition"
	PermSIProjects        Permission = "si:projects"
	PermSIManage         Permission = "si:manage"
)

var rolePermissions = map[Role][]Permission{
	RoleViewer: {
		PermOrderList, PermOrderView, PermAuditView,
		PermCommentRead, PermNotifyRead,
		PermSIList, PermSIView,
	},
	RoleOperator: {
		PermOrderList, PermOrderView, PermAuditView,
		PermOrderCreate, PermOrderTransition, PermHoldCreate, PermHoldResolve,
		PermCommentRead, PermCommentPost, PermNotifyRead, PermScanUse, PermPickUse,
		PermSIList, PermSIView,
	},
	RoleQC: {
		PermOrderList, PermOrderView, PermAuditView, PermQCSubmit,
		PermCommentRead, PermCommentPost, PermNotifyRead,
		PermSIList, PermSIView,
	},
	RoleNPI: {
		PermSIList, PermSIView, PermSICreate, PermSIUpdate, PermSITransition, PermSIProjects,
	},
	RoleAdmin: {
		PermOrderList, PermOrderView, PermAuditView,
		PermOrderCreate, PermOrderTransition, PermHoldCreate, PermHoldResolve,
		PermQCSubmit, PermUsersManage, PermManualManage,
		PermCommentRead, PermCommentPost, PermNotifyRead, PermScanUse, PermPickUse,
		PermSIList, PermSIView, PermSICreate, PermSIUpdate, PermSITransition, PermSIProjects, PermSIManage,
	},
}

func HasPermission(role Role, perm Permission) bool {
	if role == RoleAdmin {
		return true
	}
	for _, p := range rolePermissions[role] {
		if p == perm {
			return true
		}
	}
	return false
}

type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	DisplayName  string    `json:"displayName"`
	Role         Role      `json:"role"`
	CreatedAt    time.Time `json:"createdAt"`
}
