package source

import (
	"bufio"
	"context"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/italombs/ParamLens/internal/engine"
)

var exportEscaper = strings.NewReplacer(`\`, `\\`, "\r", `\r`, "\n", `\n`, "\t", `\t`)

// WriteDemoTSV writes n events of the synthetic sample data (RunDemo) as a "ParamLens TSV v1" file
// (docs/export-format.md), with a header and timestamps over one hour. It is the sample file of the
// online demo of ParamLens.html.
func WriteDemoTSV(w io.Writer, n int) error {
	bw := bufio.NewWriter(w)
	bw.WriteString("event\ttimestamp\tduration\tcpu_time\tlogical_reads\tphysical_reads\twrites\trow_count\tstatement\n")
	t0 := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	step := time.Hour / time.Duration(max(n, 1))
	i := 0
	err := RunDemo(context.Background(), n, func(r engine.Row) {
		bw.WriteString("rpc_completed\t")
		bw.WriteString(t0.Add(time.Duration(i) * step).Format("2006-01-02T15:04:05.000"))
		for k := 0; k < engine.NMetrics; k++ {
			bw.WriteByte('\t')
			if r.Has[k] {
				bw.WriteString(strconv.FormatInt(r.M[k], 10))
			}
		}
		bw.WriteByte('\t')
		bw.WriteString(exportEscaper.Replace(r.Stmt))
		bw.WriteByte('\n')
		i++
	})
	if err != nil {
		return err
	}
	return bw.Flush()
}
