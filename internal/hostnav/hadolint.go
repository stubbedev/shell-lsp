package hostnav

import (
	"github.com/stubbedev/shell-lsp/internal/hadolint"
	"github.com/stubbedev/shell-lsp/internal/host"
	"github.com/stubbedev/shell-lsp/internal/host/dockerfile"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

type Hadolint struct {
	file        *workspace.File
	dockerfile  *dockerfile.File
	config      hadolint.Config
	suppression hadolint.Suppression
	embedded    []embeddedDockerfile
}

func HadolintOf(file *workspace.File, cfg hadolint.Config) (Hadolint, bool) {
	if file.Native == nil {
		return Hadolint{}, false
	}
	f, ok := file.Native.Dockerfile()
	if !ok {
		return embeddedHadolint(file, cfg)
	}
	return Hadolint{file: file, dockerfile: f, config: cfg, suppression: hadolint.NewSuppression(f, cfg)}, true
}

func embeddedHadolint(file *workspace.File, cfg hadolint.Config) (Hadolint, bool) {
	if file.Native.Language != host.Nix || file.Native.Hosts() == 0 {
		return Hadolint{}, false
	}
	embedded := embeddedDockerfiles(file, cfg)
	return Hadolint{file: file, embedded: embedded}, len(embedded) > 0
}

func (h Hadolint) runs() []*hadolint.Run {
	return workspace.Memo(h.file, "hostnav.hadolint.runs", 0, func() []*hadolint.Run {
		variants := h.file.Variants()
		scripts := make([]hadolint.Script, len(variants))
		for i, v := range variants {
			scripts[i] = hadolint.Script{Tree: v.Tree, Doc: v.Doc}
		}
		read := make([]parser.Span, len(h.file.Native.Index.Shells))
		for i, s := range h.file.Native.Index.Shells {
			read[i] = s.Span
		}
		return hadolint.Runs(h.dockerfile, scripts, read)
	})
}

func (h Hadolint) Findings() []lint.Finding {
	if h.dockerfile == nil {
		var out []lint.Finding
		for _, d := range h.embedded {
			out = append(out, d.findings()...)
		}
		return out
	}
	return hadolint.Check(hadolint.Input{File: h.dockerfile, Runs: h.runs(), Config: h.config})
}

func (h Hadolint) Host(findings []lint.Finding) []lint.Finding {
	if h.dockerfile == nil {
		return h.suppressEmbedded(findings, func(offset int) int { return offset })
	}
	return h.suppression.Apply(findings, func(offset int) int { return offset })
}

func (h Hadolint) Shell(findings []lint.Finding) []lint.Finding {
	if h.dockerfile == nil {
		if len(h.embedded) == 0 {
			return findings
		}
		return h.suppressEmbedded(findings, h.file.Doc.SourceOffset)
	}
	return h.suppression.Apply(findings, h.file.Doc.SourceOffset)
}

func (h Hadolint) suppressEmbedded(findings []lint.Finding, hostOffset func(int) int) []lint.Finding {
	if len(h.embedded) == 0 {
		return findings
	}
	out := make([]lint.Finding, 0, len(findings))
	for _, f := range findings {
		kept := []lint.Finding{f}
		for _, d := range h.embedded {
			if applied, inside := d.suppress(f, hostOffset(f.Span.Start)); inside {
				kept = applied
				break
			}
		}
		out = append(out, kept...)
	}
	return out
}
