package server

import (
	"context"
	"strconv"
)

type progressCreateParams struct {
	Token string `json:"token"`
}

type progressParams struct {
	Token string        `json:"token"`
	Value progressValue `json:"value"`
}

type progressValue struct {
	Kind       string `json:"kind"`
	Title      string `json:"title,omitempty"`
	Message    string `json:"message,omitempty"`
	Percentage int    `json:"percentage,omitempty"`
}

type Progress struct {
	server *Server
	token  string
}

func (s *Server) StartProgress(ctx context.Context, title string) (*Progress, error) {
	token := "shell-lsp/progress-" + strconv.FormatInt(s.nextID.Add(1), 10)
	if err := s.call(ctx, "window/workDoneProgress/create", progressCreateParams{Token: token}, nil); err != nil {
		return nil, err
	}
	p := &Progress{server: s, token: token}
	return p, p.notify(progressValue{Kind: "begin", Title: title})
}

func (p *Progress) Report(message string, percentage int) error {
	return p.notify(progressValue{Kind: "report", Message: message, Percentage: percentage})
}

func (p *Progress) End(message string) error {
	return p.notify(progressValue{Kind: "end", Message: message})
}

func (p *Progress) notify(value progressValue) error {
	return p.server.conn.Notify("$/progress", progressParams{Token: p.token, Value: value})
}
