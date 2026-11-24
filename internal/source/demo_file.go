package source

import (
	"bufio"
	"context"
	"io"
	"strconv"
	"strings"

	"github.com/italombs/ParamLens/internal/engine"
)

var exportEscaper = strings.NewReplacer(`\`, `\\`, "\r", `\r`, "\n", `\n`, "\t", `\t`)

// WriteDemoTSV writes n events of the synthetic sample data (RunDemo) as a "ParamLens TSV v1" file
// (docs/export-format.md), with a header and the timestamps of the sample data. It is the sample file of the
// online demo of ParamLens.html.
func WriteDemoTSV(w io.Writer, n int) error {
	bw := bufio.NewWriter(w)
	bw.WriteString("event\ttimestamp\tduration\tcpu_time\tlogical_reads\tphysical_reads\twrites\trow_count\tstatement\n")
	err := RunDemo(context.Background(), n, func(r engine.Row) {
		bw.WriteString("rpc_completed\t")
		bw.WriteString(r.TS)
		for k := 0; k < engine.NMetrics; k++ {
			bw.WriteByte('\t')
			if r.Has[k] {
				bw.WriteString(strconv.FormatInt(r.M[k], 10))
			}
		}
		bw.WriteByte('\t')
		bw.WriteString(exportEscaper.Replace(r.Stmt))
		bw.WriteByte('\n')
	})
	if err != nil {
		return err
	}
	return bw.Flush()
}
