package agents

import (
	"context"
	"fmt"
	"strings"
	"uuid"

	"github.com/tanq16/isane/internal/store"
)

const historyLimit = 50

const fetchInstruction = "To read any attachment listed above, run: ./fetch-attachment <id>\n" +
	"It writes the file into ./files/ and prints the path. Fetch only what you need."

const outputInstruction = "Your answer is posted as a message and rendered as markdown. " +
	"GFM tables are supported, and a ```mermaid fence renders as a diagram."

func (s *Service) ComposePrompt(ctx context.Context, a store.AgentInfo, m store.Message, c store.Container) (string, []uuid.UUID, error) {
	p := &prompt{seen: make(map[uuid.UUID]bool)}
	p.add(fmt.Sprintf("You are @%s in %s.", a.User.Handle, containerLabel(c)))
	p.add("The message addressed to you:")
	p.add(p.render(m.Body, m.Attachments))

	if a.Agent.AllowHistory || m.ThreadRootID != nil {
		handles, err := s.handles(ctx)
		if err != nil {
			return "", nil, err
		}
		p.handles = handles
	}
	if a.Agent.AllowHistory {
		history, err := s.db.PromptHistory(ctx, m.ContainerID, m.Seq, historyLimit)
		if err != nil {
			return "", nil, fmt.Errorf("compose prompt for %s: %w", a.User.Handle, err)
		}
		if body := p.transcript(history, m.ID); body != "" {
			p.add("Recent conversation, oldest first:")
			p.add(body)
		}
	}
	if m.ThreadRootID != nil {
		thread, err := s.db.ThreadHistory(ctx, *m.ThreadRootID)
		if err != nil {
			return "", nil, fmt.Errorf("compose prompt for %s: %w", a.User.Handle, err)
		}
		if body := p.transcript(thread, m.ID); body != "" {
			p.add("This is a reply in a thread. The thread so far:")
			p.add(body)
		}
	}

	if len(p.attachments) > 0 {
		p.add(fetchInstruction)
	}
	p.add(outputInstruction)
	return strings.Join(p.parts, "\n\n"), p.attachments, nil
}

func (s *Service) handles(ctx context.Context) (map[uuid.UUID]string, error) {
	users, err := s.db.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve handles: %w", err)
	}
	out := make(map[uuid.UUID]string, len(users))
	for _, u := range users {
		out[u.ID] = u.Handle
	}
	return out, nil
}

type prompt struct {
	handles     map[uuid.UUID]string
	parts       []string
	attachments []uuid.UUID
	seen        map[uuid.UUID]bool
}

func (p *prompt) add(part string) { p.parts = append(p.parts, part) }

func (p *prompt) render(body string, attachments []store.Attachment) string {
	lines := []string{body}
	for _, at := range attachments {
		if !p.seen[at.ID] {
			p.seen[at.ID] = true
			p.attachments = append(p.attachments, at.ID)
		}
		lines = append(lines, fmt.Sprintf("[attachment id=%s name=%s size=%s]",
			at.ID, at.OriginalName, formatSize(at.SizeBytes)))
	}
	return strings.Join(lines, "\n")
}

func (p *prompt) transcript(msgs []store.Message, skip uuid.UUID) string {
	var lines []string
	for _, msg := range msgs {
		if msg.ID == skip || msg.DeletedAt != nil {
			continue
		}
		lines = append(lines, p.render("@"+p.handle(msg.AuthorID)+": "+msg.Body, msg.Attachments))
	}
	return strings.Join(lines, "\n")
}

func (p *prompt) handle(id uuid.UUID) string {
	if h, ok := p.handles[id]; ok {
		return h
	}
	return "unknown"
}

func containerLabel(c store.Container) string {
	if c.Kind == store.ContainerConversation {
		return "a direct conversation"
	}
	switch {
	case c.Name != nil && *c.Name != "":
		return "the " + *c.Name + " channel"
	case c.Slug != nil && *c.Slug != "":
		return "the " + *c.Slug + " channel"
	}
	return "a channel"
}

func formatSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGT"[exp])
}
