package model

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestAuditOtherRoundTripKeepsLargeIntegers(t *testing.T) {
	other := AuditOther{
		Op:        &AuditOperation{Action: "user.manage", Params: AuditFields{"quota": int64(9007199254740993)}},
		AdminInfo: &AuditAdminInfo{AdminID: 1, AdminRole: common.RoleRootUser},
		RootInfo:  AuditFields{"node": "blue"},
	}
	value, err := other.Value()
	require.NoError(t, err)

	var decoded AuditOther
	require.NoError(t, decoded.Scan(value))
	encoded, err := common.Marshal(decoded.Op.Params)
	require.NoError(t, err)
	require.JSONEq(t, `{"quota":9007199254740993}`, string(encoded))
	require.Equal(t, 1, decoded.AdminInfo.AdminID)

	var empty AuditOther
	require.NoError(t, empty.Scan(nil))
	require.Nil(t, empty.Op)
}

func TestAuditFilterValidation(t *testing.T) {
	require.True(t, ValidAuditCategory(""))
	require.True(t, ValidAuditCategory(AuditCategoryAccessToken))
	require.False(t, ValidAuditCategory("usage"))
	require.True(t, ValidTokenFingerprint(""))
	require.True(t, ValidTokenFingerprint(AccessTokenFingerprint("nap_example")))
	require.False(t, ValidTokenFingerprint("ABC"))
	require.Equal(t, AccessTokenFingerprint("abc"), AccessTokenFingerprint("abc   "))
}

func TestAuditKnownActorRoleIncludesEflowRoles(t *testing.T) {
	for _, role := range []int{common.RoleCommonUser, common.RoleCustomerServiceUser, common.RoleFinanceUser, common.RoleAdminUser, common.RoleRootUser} {
		require.True(t, auditKnownActorRole(role), "role %d", role)
	}
	require.False(t, auditKnownActorRole(common.RoleGuestUser))
	require.False(t, auditKnownActorRole(42))
}

func TestAuditLogsVisibilityProjection(t *testing.T) {
	useModelIntegrationTestDB(t, &User{}, &AuditLog{})
	financeRole := common.RoleFinanceUser
	for _, entry := range []AuditLog{
		{UserId: 7, Username: "finance", ActorRole: financeRole, Category: AuditCategoryOperation, Action: "user.manage",
			Other: AuditOther{AdminInfo: &AuditAdminInfo{AdminID: 7}, AuditInfo: &AuditRequestInfo{Route: "/api/user/manage"}, RootInfo: AuditFields{"node": "blue"}}},
		{UserId: 1, Username: "root", ActorRole: common.RoleRootUser, Category: AuditCategorySecurity, Action: "user.2fa_enable"},
		{UserId: 9, Username: "unknown", ActorRole: 42, Category: AuditCategoryLogin, Action: "user.login"},
	} {
		RecordAuditLog(nil, entry)
	}

	adminLogs, total, err := GetAuditLogs(AuditLogFilter{}, 0, 10, common.RoleAdminUser)
	require.NoError(t, err)
	require.EqualValues(t, 1, total, "admins only see events by non-root actors with a known role")
	require.Equal(t, "finance", adminLogs[0].Username)
	require.NotNil(t, adminLogs[0].Other.AdminInfo)
	require.Nil(t, adminLogs[0].Other.RootInfo)

	rootLogs, total, err := GetAuditLogs(AuditLogFilter{}, 0, 10, common.RoleRootUser)
	require.NoError(t, err)
	require.EqualValues(t, 3, total)
	for _, entry := range rootLogs {
		if entry.Username == "finance" {
			require.NotNil(t, entry.Other.RootInfo)
		}
		if entry.Username == "unknown" {
			require.Zero(t, entry.ActorRole, "unknown actor roles are stored as 0")
		}
	}

	selfLogs, total, err := GetAuditLogs(AuditLogFilter{UserId: 7, SelfView: true}, 0, 10, common.RoleRootUser)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Nil(t, selfLogs[0].Other.AdminInfo, "self view strips privileged metadata even for root")
	require.Nil(t, selfLogs[0].Other.AuditInfo)
	require.Nil(t, selfLogs[0].Other.RootInfo)
}

// TestAuditLogsAreNeverDeleted guards SPEC hard rule 3: audit records are
// permanent. No production code may delete, truncate or expire audit_logs,
// the usage/request log table logs, or the application log files in LogDir.
func TestAuditLogsAreNeverDeleted(t *testing.T) {
	root, err := filepath.Abs("..")
	require.NoError(t, err)
	var offenders []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "web", "node_modules", ".git", ".ai-governance", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			return nil
		}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil && removesLogDirFiles(fn.Body) {
				offenders = append(offenders, path+":"+fn.Name.Name)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Delete" {
				for _, arg := range call.Args {
					if referencesAuditLog(arg) {
						offenders = append(offenders, path)
					}
				}
			}
			for _, arg := range call.Args {
				if lit, ok := arg.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					sql := strings.ToLower(lit.Value)
					names := strings.Contains(sql, "audit_logs") || strings.Contains(sql, " logs") || strings.Contains(sql, "`logs`")
					if names && (strings.Contains(sql, "delete") || strings.Contains(sql, "truncate") || strings.Contains(sql, "drop")) {
						offenders = append(offenders, path)
					}
				}
			}
			return true
		})
		return nil
	})
	require.NoError(t, err)
	require.Empty(t, offenders, "audit_logs, logs and application log files must never be deleted")
}

// removesLogDirFiles reports whether a function both touches the application
// log directory (LogDir) and calls os.Remove / os.RemoveAll.
func removesLogDirFiles(body *ast.BlockStmt) bool {
	touchesLogDir, removes := false, false
	ast.Inspect(body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.Ident:
			if n.Name == "LogDir" {
				touchesLogDir = true
			}
		case *ast.SelectorExpr:
			if pkg, ok := n.X.(*ast.Ident); ok && pkg.Name == "os" && (n.Sel.Name == "Remove" || n.Sel.Name == "RemoveAll") {
				removes = true
			}
		}
		return true
	})
	return touchesLogDir && removes
}

func referencesAuditLog(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if ident, ok := node.(*ast.Ident); ok && (ident.Name == "AuditLog" || ident.Name == "Log") {
			found = true
		}
		return !found
	})
	return found
}
