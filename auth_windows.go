//go:build windows

package main

// Autenticação Windows (SSPI) quando não há usuário/senha na conexão.
import _ "github.com/microsoft/go-mssqldb/integratedauth/winsspi"
