package push

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/tanq16/isane/internal/markdown"
	"github.com/tanq16/isane/internal/store"
)

const (
	declarativeWebPush = 8030
	bodyLimit          = 120
)

type payload struct {
	WebPush      int          `json:"web_push"`
	Notification notification `json:"notification"`
	X            ref          `json:"x"`
}

type notification struct {
	Title    string `json:"title"`
	Body     string `json:"body"`
	Navigate string `json:"navigate"`
	AppBadge string `json:"app_badge"`
}

type ref struct {
	ContainerID uuid.UUID `json:"container_id"`
	MessageID   uuid.UUID `json:"message_id"`
	Seq         int64     `json:"seq"`
}

func buildPayload(publicURL string, m store.Message, author store.User, c store.Container, badge int64) payload {
	text := markdown.Truncate(markdown.PlainText(m.Body), bodyLimit)
	title := author.DisplayName
	body := text
	if c.Kind == store.ContainerChannel {
		title = channelTitle(c)
		body = author.DisplayName
		if text != "" {
			body += ": " + text
		}
	}
	return payload{
		WebPush: declarativeWebPush,
		Notification: notification{
			Title:    title,
			Body:     body,
			Navigate: navigateURL(publicURL, m, c),
			AppBadge: strconv.FormatInt(badge, 10),
		},
		X: ref{ContainerID: m.ContainerID, MessageID: m.ID, Seq: m.Seq},
	}
}

func channelTitle(c store.Container) string {
	if c.Slug != nil && *c.Slug != "" {
		return "#" + *c.Slug
	}
	if c.Name != nil {
		return *c.Name
	}
	return ""
}

func navigateURL(publicURL string, m store.Message, c store.Container) string {
	var path string
	switch {
	case m.ThreadRootID != nil:
		path = "/t/" + m.ThreadRootID.String()
	case c.Kind == store.ContainerChannel && c.Slug != nil:
		path = "/c/" + url.PathEscape(*c.Slug)
	default:
		path = "/d/" + c.ID.String()
	}
	return strings.TrimSuffix(publicURL, "/") + path + "?m=" + strconv.FormatInt(m.Seq, 10)
}
