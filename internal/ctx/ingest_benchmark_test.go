package ctx

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func BenchmarkLoadCache(b *testing.B) {
	var data strings.Builder
	for index := 0; index < 500; index++ {
		when := time.Date(2026, 1, 1, 0, 0, index, 0, time.UTC).Add(time.Duration(index/2) * 24 * time.Hour)
		fmt.Fprintf(&data, `{"record_type":"event_range_event","event":{"ctx_event_id":"event-%d","ctx_session_id":"ctx-synthetic","event_type":"message","occurred_at":%q,"provider":"codex","provider_session_id":"provider-synthetic","role":"user","content":{"text":"prompt-%d"}}}`+"\n", index, when.Format(time.RFC3339Nano), index)
	}
	data.WriteString(`{"record_type":"event_range_completion","generation_id":"synthetic-generation","terminal":true,"truncated":false}`)
	data.WriteByte('\n')
	response := []byte(data.String())
	runner := func(args []string) (CommandResult, error) {
		if containsPair(args, "--limit", "1") {
			return CommandResult{Stdout: []byte(`{"record_type":"event_range_completion","generation_id":"synthetic-generation","terminal":true,"truncated":false}` + "\n")}, nil
		}
		return CommandResult{Stdout: response}, nil
	}
	now := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	run := func(cacheDir string, days int, daysSet bool) {
		b.Helper()
		if _, err := Load("/tmp/catsift-benchmark-ctx", IngestOptions{DataRoot: "/tmp/catsift-benchmark-ctx", Now: now, Days: days, DaysSet: daysSet, CacheDir: cacheDir, Runner: runner}); err != nil {
			b.Fatal(err)
		}
	}

	for _, test := range []struct {
		name    string
		days    int
		daysSet bool
	}{
		{name: "all", days: 0},
		{name: "days1", days: 1, daysSet: true},
	} {
		b.Run(test.name+"/cold", func(b *testing.B) {
			b.ReportAllocs()
			root := b.TempDir()
			b.ResetTimer()
			for index := 0; index < b.N; index++ {
				run(filepath.Join(root, strconv.Itoa(index)), test.days, test.daysSet)
			}
		})
		b.Run(test.name+"/warm", func(b *testing.B) {
			cacheDir := b.TempDir()
			run(cacheDir, 0, false)
			b.ReportAllocs()
			b.ResetTimer()
			for index := 0; index < b.N; index++ {
				run(cacheDir, test.days, test.daysSet)
			}
		})
	}
}
