//go:build windows

package source

// Autenticação Windows (SSPI) quando não há usuário/senha na conexão.
import _ "github.com/microsoft/go-mssqldb/integratedauth/winsspi"
