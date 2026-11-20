# ParamLens

**Perfil de parâmetros de eventos `rpc_completed` do SQL Server Extended Events.**
*Parameter profiling for SQL Server Extended Events `rpc_completed` captures.*

Você aponta para uma tabela com eventos `rpc_completed` (ou para um `.xel` no servidor) e o ParamLens
interpreta cada `exec sp_executesql …`, extrai os parâmetros e mostra como a aplicação realmente chama
cada query: distribuição de valores, combinações, custo por padrão de chamada e predicados *catch-all*
(`@p = 0 OR col = @p`) que viram *pass-through* na maioria das execuções.

Feito para os problemas típicos de ORMs/low-code (OutSystems, Entity Framework, etc.): ORs opcionais,
parameter sniffing, listas `IN` inline e variação de tipos/tamanhos de parâmetros.

## Download

Baixe o `ParamLens.exe` em [Releases](../../releases). Um único executável, sem instalação.
Ao abrir, ele inicia uma interface local em `http://127.0.0.1:<porta>` e abre o navegador.

## O que ele mostra

- **Predicados catch-all** detectados no SQL (`@x = 0 OR …`, `@d = @nullDate OR …`, `ISNULL(@p, …)`) e o
  % de execuções em que cada um é verdadeiro, com custo médio nos dois casos.
- **Formatos de chamada:** quais filtros estavam ativos juntos, com % de execuções e % do custo.
- **Parâmetros:** tipo, cardinalidade, valores mais frequentes, % NULL, classes (NULL, 0, '', data nula…).
- **Combinações** de valores escolhidas por você, exportáveis em CSV.
- **Listas IN inline** como pseudo-parâmetros (valores e tamanho) e o número de textos SQL distintos gerados.
- **Achados:** valores dominantes com custo desigual (parameter sniffing), declarações de parâmetros
  divergentes, parâmetros constantes, combinações desproporcionalmente caras.
- **Script de teste:** `DECLARE` + SQL de uma execução real, pronto para o SSMS.
- **Relatório HTML** exportável, que abre offline.

## Fontes suportadas

| Fonte | Como |
|---|---|
| Tabela | Qualquer tabela com o campo `statement` (mapeamento de colunas automático) |
| Arquivo `.xel` | Lido pelo próprio SQL Server via `sys.fn_xe_file_target_read_file` (requer `VIEW SERVER STATE`) |

Unidade de `duration`/`cpu_time`: microssegundos (padrão do XE) ou milissegundos.

## Privacidade e segurança

- Conecta somente ao SQL Server que você informar. Sem telemetria, sem chamadas externas.
- A interface escuta só em `127.0.0.1` e exige um token aleatório por sessão (também valida o `Host`).
- Senhas ficam apenas na memória do processo.
- Os statements lidos são gravados num arquivo temporário (para os exemplos) e apagados ao encerrar.

## Compilar

```bash
go test ./...
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o ParamLens.exe .
```

Opções de linha de comando: `-port 8080` (porta fixa), `-no-browser` e `-bind` (padrão `127.0.0.1`).

### Sem Go instalado (Docker)

```bash
docker build --target export --output dist .        # gera dist/ParamLens.exe (roda vet + testes)
docker build -t paramlens .                          # imagem Linux
docker run --rm -it -p 127.0.0.1:8765:8765 paramlens # abra o link "Interface:" do console
```

No container só funciona autenticação SQL (não Windows/SSPI); para o SQL Server do próprio host use
`host.docker.internal` como servidor.

## Estrutura

| Arquivo | Conteúdo |
|---|---|
| `tsql.go` | Tokenizer T-SQL (strings com `''`/`N''`, `[ident]`, comentários aninhados, `@vars`) |
| `rpc.go` | Parse de `sp_executesql`, `sp_prepexec` e `exec proc` (nomeados e posicionais) |
| `normalize.go` | Normalização do SQL em famílias, listas IN, detecção de catch-all |
| `engine.go` | Ingestão e avaliação de predicados por execução |
| `stats.go` | Estatísticas, combinações, formatos de chamada e achados |
| `source.go` | Fontes de dados (tabela, `.xel`, demo) |
| `main.go` | Servidor local e API |
| `web/index.html` | Interface (embutida no executável) |

## Code signing

Free code signing provided by [SignPath.io](https://signpath.io), certificate by [SignPath Foundation](https://signpath.org).
See [CODE_SIGNING_POLICY.md](CODE_SIGNING_POLICY.md).

## Licença

[MIT](LICENSE) © 2026 Italo Mesquita — [Data Power's](#)
