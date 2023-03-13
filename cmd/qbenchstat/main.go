package main

import (
	"bytes"
	"errors"
	"flag"
	"log"
	"math"
	"os"
	"strings"

	"golang.org/x/perf/benchstat"
)

type StringSet map[string]struct{}

func (u StringSet) Set(value string) error {
	u[value] = struct{}{}
	return nil
}

func (u StringSet) String() string {
	if len(u) == 0 {
		return "[]"
	}
	var buf strings.Builder
	buf.Grow(len(u) * 10)
	first := true
	buf.WriteByte('[')
	for t := range u {
		if first {
			first = false
		} else {
			buf.WriteByte(',')
		}
		buf.WriteString(t)
	}
	buf.WriteByte(']')
	return buf.String()
}

func (u *StringSet) Type() string {
	return "[]string"
}

func main() {
	log.SetFlags(0)
	if err := runBenchstat(); err != nil {
		log.Fatalf("error: %+v", err)
	}
}

func runBenchstat() error {
	flagDeltaTest := flag.String("delta-test", "utest", "significance `test` to apply to delta: utest, ttest, or none")
	flagAlpha := flag.Float64("alpha", 0.05, "consider change significant if p < `α`")
	flagGeomean := flag.Bool("geomean", false, "print the geometric mean of each file")
	flagSplit := flag.String("split", "pkg,goos,goarch", "split benchmarks by `labels`")
	flagSort := flag.String("sort", "none", "sort by `order`: [-]delta, [-]name, none")
	noColor := flag.Bool("no-color", false, "disable the colored output")
	increasing := make(StringSet)
	flag.Var(increasing, "increasing", "metrics where increasing is better")
	flag.Parse()

	colorsEnabled := !*noColor

	var deltaTestNames = map[string]benchstat.DeltaTest{
		"none":   benchstat.NoDeltaTest,
		"u":      benchstat.UTest,
		"u-test": benchstat.UTest,
		"utest":  benchstat.UTest,
		"t":      benchstat.TTest,
		"t-test": benchstat.TTest,
		"ttest":  benchstat.TTest,
	}

	var sortNames = map[string]benchstat.Order{
		"none":  nil,
		"name":  benchstat.ByName,
		"delta": benchstat.ByDelta,
	}

	deltaTest := deltaTestNames[strings.ToLower(*flagDeltaTest)]
	if deltaTest == nil {
		return errors.New("invalid delta-test argument")
	}
	sortName := *flagSort
	reverse := false
	if strings.HasPrefix(sortName, "-") {
		reverse = true
		sortName = sortName[1:]
	}
	order, ok := sortNames[sortName]
	if !ok {
		return errors.New("invalid sort argument")
	}

	if len(flag.Args()) == 0 {
		// TODO: print command help here?
		return errors.New("expected at least 1 positional argument, the benchmarking target")
	}

	c := &benchstat.Collection{
		Alpha:      *flagAlpha,
		AddGeoMean: *flagGeomean,
		DeltaTest:  deltaTest,
	}
	if *flagSplit != "" {
		c.SplitBy = strings.Split(*flagSplit, ",")
	}
	if order != nil {
		if reverse {
			order = benchstat.Reverse(order)
		}
		c.Order = order
	}
	for _, file := range flag.Args() {
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		if err := c.AddFile(file, f); err != nil {
			return err
		}
		f.Close()
	}

	tables := c.Tables()
	fixBenchstatTables(tables)
	if colorsEnabled {
		colorizeBenchstatTables(tables, increasing)
	}
	var buf bytes.Buffer
	benchstat.FormatText(&buf, tables)
	os.Stdout.Write(buf.Bytes())

	return nil
}

func calculateMeanDiff(m *benchstat.Metrics) float64 {
	if m.Mean == 0 || m.Max == 0 {
		return 0
	}
	diff := 1 - m.Min/m.Mean
	if d := m.Max/m.Mean - 1; d > diff {
		diff = d
	}
	return diff
}

func calculateCombinedMeanDiff(metrics []*benchstat.Metrics) float64 {
	d := 0.0
	for _, m := range metrics {
		if m.Max == m.Min {
			continue
		}
		d += 100.0 * calculateMeanDiff(m)
	}
	return d
}

func isTinyValue(metrics []*benchstat.Metrics) bool {
	const tinyValueThreshold = 32.0 // in nanosecs
	for _, m := range metrics {
		if m.Mean >= tinyValueThreshold {
			return false
		}
	}
	return true
}

func avgValue(metrics []*benchstat.Metrics) float64 {
	v := 0.0
	for _, m := range metrics {
		v += m.Mean
	}
	return v / float64(len(metrics))
}

func getValueEpsilon(avg float64) float64 {
	switch {
	case avg < 10:
		return 1
	case avg < 32:
		return 2
	case avg < 80:
		return 3
	default:
		return 4
	}
}

func isEpsilonDelta(metrics []*benchstat.Metrics) bool {
	if len(metrics) != 2 {
		return false
	}
	eps := getValueEpsilon(avgValue(metrics))
	return math.Abs(metrics[0].Mean-metrics[1].Mean) <= eps
}

func colorizeBenchstatTables(tables []*benchstat.Table, increasing StringSet) {
	for _, table := range tables {
		_, isIncreasingIsBetter := increasing[table.Metric]
		for _, row := range table.Rows {
			if isEpsilonDelta(row.Metrics) {
				row.Delta = yellowColorize("~")
				continue
			}
			d := calculateCombinedMeanDiff(row.Metrics)
			if isTinyValue(row.Metrics) {
				// For tiny values, require x2 precision.
				d *= 2
			}
			d++
			if math.Abs(row.PctDelta) < d {
				row.Delta = yellowColorize("~")
				continue
			}
			if strings.HasPrefix(row.Delta, "+") {
				if isIncreasingIsBetter {
					row.Delta = greenColorize(row.Delta)
				} else {
					row.Delta = redColorize(row.Delta)
				}
			} else if strings.HasPrefix(row.Delta, "-") {
				if isIncreasingIsBetter {
					row.Delta = redColorize(row.Delta)
				} else {
					row.Delta = greenColorize(row.Delta)
				}
			} else {
				row.Delta = yellowColorize(row.Delta)
			}
		}
	}
}

func fixBenchstatTables(tables []*benchstat.Table) {
	disabledGeomean := map[string]struct{}{}
	for _, table := range tables {
		selectedRows := table.Rows[:0]
		for _, row := range table.Rows {
			if row.PctDelta == 0 && strings.Contains(row.Delta, "0.00%") {
				// For whatever reason, sometimes we get +0.00% results
				// in delta which will be painted red. This is misleading.
				// Let's replace +0.00% with tilde.
				row.Delta = "~"
			}
			for _, m := range row.Metrics {
				for _, v := range m.RValues {
					if v < 0.01 {
						disabledGeomean[m.Unit] = struct{}{}
					}
				}
			}
			if row.Benchmark == "[Geo mean]" {
				if len(row.Metrics) != 0 {
					_, disabled := disabledGeomean[row.Metrics[0].Unit]
					if disabled {
						continue
					}
				}
			}
			selectedRows = append(selectedRows, row)
			if len(row.Metrics) == 0 {
				continue
			}
			if len(row.Metrics[0].RValues) < 5 && row.Benchmark != "[Geo mean]" {
				log.Printf("WARNING: %s needs more samples, re-run with -count=5 or higher?", row.Benchmark)
			}
		}
		table.Rows = selectedRows
	}
}
