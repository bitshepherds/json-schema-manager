package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/bitshepherds/json-schema-manager/internal/consumer"
	"github.com/bitshepherds/json-schema-manager/internal/schema"
)

// WriteConsumerCheck writes the result of checking a consumer manifest.
func (tr *TextReporter) WriteConsumerCheck(w io.Writer, res *consumer.Result) error {
	divider := strings.Repeat("-", 40)
	_, _ = fmt.Fprintf(w, "%s\n%s %s\n%s\n", divider, tr.cs(colBoldWhite, "JSM CONSUMER CHECK:"), res.Manifest, divider)

	for i := range res.Schemas {
		sr := &res.Schemas[i]
		statusCol := map[consumer.Status]string{
			consumer.Pass: colGreen, consumer.Warn: colYellow, consumer.Fail: colRed,
		}[sr.Status()]
		_, _ = fmt.Fprintf(w, "%s %s\n", tr.cs(statusCol, "["+strings.ToUpper(string(sr.Status()))+"]"), sr.Entry)

		if sr.Problem != "" {
			_, _ = fmt.Fprintf(w, "  %s %s\n", tr.cs(colRed, "✗"), sr.Problem)
		}
		for _, rej := range sr.Rejections {
			_, _ = fmt.Fprintf(w, "  %s %s sends a document which %s rejects: %s\n",
				tr.cs(colRed, "✗"), rej.Later, sr.Key, tr.cs(colGrey, rej.Path))
			_, _ = fmt.Fprintf(w, "    %v\n", rej.Err)
		}
		if sr.NewerMajor != "" {
			_, _ = fmt.Fprintf(w, "  %s the family has a newer major version, %s: move to it before providers "+
				"stop sending version %d\n", tr.cs(colYellow, "!"), sr.NewerMajor, sr.Key.Major())
		}
	}

	failed, warned := res.Count(consumer.Fail), res.Count(consumer.Warn)
	statsCol := colBoldGreen
	if failed > 0 {
		statsCol = colBoldRed
	}
	stats := fmt.Sprintf("%d passed, %d failed, %d %s", res.Count(consumer.Pass), failed, warned,
		plural(warned, "warning"))
	_, err := fmt.Fprintf(w, "%s\n%s%s\n%s\n", divider, tr.cs(colBoldWhite, "Consumer check summary: "),
		tr.cs(statsCol, stats), divider)
	return err
}

type jsonRejection struct {
	Later schema.Key `json:"later"` // the later version the document belongs to
	Path  string     `json:"path"`
	Error string     `json:"error"`
}

type jsonConsumerSchema struct {
	Entry      string          `json:"entry"`
	Key        schema.Key      `json:"key,omitempty"`
	Status     consumer.Status `json:"status"`
	Problem    string          `json:"problem,omitempty"`
	Rejections []jsonRejection `json:"rejections"`
	NewerMajor schema.Key      `json:"newerMajor,omitempty"`
}

type jsonConsumerCheck struct {
	Manifest string `json:"manifest"`
	Stats    struct {
		Passed   int `json:"passed"`
		Failed   int `json:"failed"`
		Warnings int `json:"warnings"`
	} `json:"stats"`
	Schemas []jsonConsumerSchema `json:"schemas"`
}

// WriteConsumerCheck writes the result of checking a consumer manifest.
func (jr *JSONReporter) WriteConsumerCheck(w io.Writer, res *consumer.Result) error {
	out := jsonConsumerCheck{Manifest: res.Manifest, Schemas: []jsonConsumerSchema{}}
	out.Stats.Passed = res.Count(consumer.Pass)
	out.Stats.Failed = res.Count(consumer.Fail)
	out.Stats.Warnings = res.Count(consumer.Warn)

	for i := range res.Schemas {
		sr := &res.Schemas[i]
		js := jsonConsumerSchema{
			Entry:      sr.Entry,
			Key:        sr.Key,
			Status:     sr.Status(),
			Problem:    sr.Problem,
			Rejections: []jsonRejection{},
			NewerMajor: sr.NewerMajor,
		}
		for _, rej := range sr.Rejections {
			js.Rejections = append(
				js.Rejections,
				jsonRejection{Later: rej.Later, Path: rej.Path, Error: rej.Err.Error()},
			)
		}
		out.Schemas = append(out.Schemas, js)
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
