# ParamLens — contexto para o Claude Code

## O que é
Ferramenta para DBA (Italo Mesquita, Data Power's). Lê eventos `rpc_completed` do Extended Events
(tabela ou `.xel`), interpreta cada `exec sp_executesql ...` e mostra como a aplicação chama cada query:
distribuição de valores dos parâmetros, combinações, custo por padrão e, principalmente, predicados
*catch-all* do tipo `@p = 0 OR col = @p` que viram *pass-through* na maioria das execuções
(caso típico do OutSystems: ORs opcionais, LEFT JOIN que poderia ser NOT EXISTS, listas IN inline).

Substitui a query manual em `docs/query-manual-original.sql` (CHARINDEX + STRING_SPLIT), que quebrava
com vírgulas em strings, `',@` dentro do SQL, várias listas IN etc.

## Requisitos do usuário
- Um único `.exe` para Windows, sem instalar nada (estilo SQLQueryStress).
- Interface em português (pt-BR), números no formato pt-BR.
- Dados sensíveis (os statements contêm IDs de pacientes): nada sai da máquina, sem telemetria.

## Arquitetura (Go 1.24, sem CGO)
- Escolhido Go em vez de .NET porque gera um `.exe` estático de ~8 MB e cross-compila sem runtime packs.
- Driver: `github.com/microsoft/go-mssqldb` **v1.8.2** (v1.9+ exige Go 1.25). Auth Windows via SSPI
  (`auth_windows.go`); a conexão é montada como URL `sqlserver://` (o parser ADO do driver não aceita `{}`,
  senha com `;` quebraria).
- O `.exe` sobe um HTTP em `127.0.0.1:<porta aleatória>`, abre o navegador e exige um token por sessão
  (header `X-Token` ou `?t=` para downloads) e valida o `Host` (anti DNS-rebinding).
- A interface é um único `web/index.html` (vanilla JS, sem CDN), embutido via `//go:embed`.
- Exportação: o mesmo `index.html` com o JSON da análise injetado no marcador `<!--STATIC-DATA-->`
  (modo estático: sem API, combinações fixas, sem botão "exemplo").

| Arquivo | Papel |
|---|---|
| `tsql.go` | Tokenizer T-SQL tolerante (strings `''`/`N''`, `[ident]]`, `"ident"`, comentários aninhados, `@vars`, números, binário) |
| `rpc.go` | `ParseRPC`: `sp_executesql`, `sp_prepexec` (com prelúdio `declare @p1 ... select @p1`), `exec proc`; parâmetros nomeados e posicionais; `ParseDecl` para tipos |
| `normalize.go` | `NormalizeSQL` (família = hash do SQL normalizado; listas `IN (literais)` viram `IN (#lista)`), `DetectPatterns` (catch-all OR, var=lit, var=var, IS [NOT] NULL, ISNULL/COALESCE) |
| `engine.go` | Ingestão: famílias, dicionário de valores por parâmetro (máx. 50k distintos), pseudo-parâmetros para listas IN (valores e #itens), statements em arquivo temporário (offset/len) para "exemplo"; `sqlEqual` aproxima a semântica do SQL Server (CI, conversão implícita, NULL); `TestScript` gera DECLARE + SQL |
| `stats.go` | Overview, detalhe por família, estatísticas de parâmetro (cardinalidade constante/baixa(≤30)/alta + classes), combinações, "formatos de chamada" (filtra/pass-through por catch-all), achados com severidade 1–3 |
| `source.go` | Fontes: tabela (mapeamento de colunas, `TRY_CAST`, filtros LIKE/WHERE/TOP), `.xel` via `sys.fn_xe_file_target_read_file` + XML, e `runDemo` (dados sintéticos estilo OutSystems) |
| `main.go` | Servidor, endpoints `/api/*`, jobs de análise em goroutine, exportação |

Métricas internas: `duration` e `cpu_time` em **microssegundos** (padrão do XE; opção para ms na fonte).
Métrica de custo padrão: logical reads → CPU → duração (a primeira disponível).

## Estado atual
- Testes unitários do parser passam (`go test ./...`). O fluxo completo foi testado com o modo demo
  (25k e 400k linhas: ~17 s, ~245 MB RAM) e a UI foi validada no Chrome headless.
- **Nunca rodou contra um SQL Server real.** Pontos a validar primeiro: conexão (Windows/SQL auth,
  instância nomeada, encrypt), listagem de tabelas/colunas, query da fonte tabela, query do `.xel`.
- `go.sum` ainda não foi gerado (rodar `go mod tidy` e commitar).
- `OWNER` em `go.mod` e `CODE_SIGNING_POLICY.md` deve ser trocado pelo usuário do GitHub.
- No PC da empresa o `.exe` não assinado é bloqueado pelo SmartScreen **sem** opção "Run anyway".

## Próximos passos
1. Publicar no GitHub (público, MIT), primeira release via tag `v1.0.0` (`.github/workflows/release.yml`)
   e candidatar o projeto em https://signpath.org/apply. O passo de assinatura do workflow só roda
   quando existir a variável `SIGNPATH_ORGANIZATION_ID` (e o secret `SIGNPATH_API_TOKEN`).
2. **Versão 100% HTML** (para usar já, sem executável): portar tokenizer/parser/estatísticas para JS
   dentro de um único `.html` que roda offline; o usuário exporta os eventos e arrasta o arquivo.
   - Exportação com uma linha por evento (escapar `\`, CR, LF, TAB no T-SQL), por `bcp queryout`
     (sem limite) ou "Save results as" do SSMS (corta células >65.535 caracteres: detectar e avisar).
   - Reaproveitar a UI de `web/index.html`; idealmente uma base de código única (motor JS também
     usado pelo modo estático do relatório).
3. Ideias: filtro por janela de tempo, comparação entre duas capturas, detectar `LEFT JOIN` usado só
   como filtro (candidato a `NOT EXISTS`), ícone do executável.

## Convenções
- Textos da UI e mensagens em pt-BR; código e identificadores em inglês.
- Sem dependências externas no front-end (tem que abrir offline).
- Toda mudança no parser vem com caso em `parse_test.go`.
- Build Windows: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o ParamLens.exe .`
- Rodar local: `go run . -port 8765` e usar "Carregar dados de exemplo" para testar a UI sem servidor.
