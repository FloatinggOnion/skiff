package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"html/template"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	netmail "net/mail"
	"net/textproto"
	"strings"
	"time"
)

// Letter is an email Skiff writes itself, as opposed to mail it forwards. Every one goes out in the same branded layout.
type Letter struct {
	Subject string
	Heading string
	Body    []string
	Button  string
	URL     string
	Note    string
	// Reason says why the recipient got this, in the footer.
	Reason string
}

//go:embed mark.png
var mark []byte

//go:embed letter.html
var letterHTML string

var letterTmpl = template.Must(template.New("letter").Parse(letterHTML))

const markID = "skiff-mark@useskiff.xyz"

// Send delivers a letter through the relay, from "Skiff".
func (r *Relay) Send(ctx context.Context, to string, l Letter) error {
	data, err := l.Compose(r.From, to, time.Now())
	if err != nil {
		return err
	}
	return (&Forwarder{Relay: r}).send(ctx, job{from: r.From, to: []string{to}, data: data})
}

// Compose renders the full message: plain text and HTML alternatives, with the mark attached inline so it shows without loading remote images.
func (l Letter) Compose(from, to string, now time.Time) ([]byte, error) {
	var page bytes.Buffer
	if err := letterTmpl.Execute(&page, l); err != nil {
		return nil, err
	}
	var altBody bytes.Buffer
	alt := multipart.NewWriter(&altBody)
	if err := writeQP(alt, "text/plain; charset=utf-8", l.text()); err != nil {
		return nil, err
	}
	if err := writeQP(alt, "text/html; charset=utf-8", page.String()); err != nil {
		return nil, err
	}
	if err := alt.Close(); err != nil {
		return nil, err
	}

	var body bytes.Buffer
	related := multipart.NewWriter(&body)
	part, err := related.CreatePart(textproto.MIMEHeader{"Content-Type": {`multipart/alternative; boundary="` + alt.Boundary() + `"`}})
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(altBody.Bytes()); err != nil {
		return nil, err
	}
	img, err := related.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {`image/png; name="skiff.png"`},
		"Content-Transfer-Encoding": {"base64"},
		"Content-ID":                {"<" + markID + ">"},
		"Content-Disposition":       {`inline; filename="skiff.png"`},
	})
	if err != nil {
		return nil, err
	}
	enc := base64.StdEncoding.EncodeToString(mark)
	for len(enc) > 76 {
		_, _ = img.Write([]byte(enc[:76] + "\r\n"))
		enc = enc[76:]
	}
	_, _ = img.Write([]byte(enc + "\r\n"))
	if err := related.Close(); err != nil {
		return nil, err
	}

	head := []string{
		"From: " + (&netmail.Address{Name: "Skiff", Address: from}).String(),
		"To: " + to,
		"Subject: " + mime.QEncoding.Encode("utf-8", l.Subject),
		"Date: " + now.Format(time.RFC1123Z),
		"Message-ID: <" + randomID() + "@" + domainOf(from) + ">",
		"MIME-Version: 1.0",
		`Content-Type: multipart/related; type="multipart/alternative"; boundary="` + related.Boundary() + `"`,
	}
	return append([]byte(strings.Join(head, "\r\n")+"\r\n\r\n"), body.Bytes()...), nil
}

func (l Letter) text() string {
	var b strings.Builder
	b.WriteString(l.Heading + "\n\n")
	for _, p := range l.Body {
		b.WriteString(p + "\n\n")
	}
	if l.Button != "" {
		b.WriteString(l.Button + ": " + l.URL + "\n\n")
	}
	if l.Note != "" {
		b.WriteString(l.Note + "\n\n")
	}
	b.WriteString("-- \nSkiff\n")
	if l.Reason != "" {
		b.WriteString(l.Reason + "\n")
	}
	return b.String()
}

func writeQP(w *multipart.Writer, contentType, body string) error {
	part, err := w.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {contentType},
		"Content-Transfer-Encoding": {"quoted-printable"},
	})
	if err != nil {
		return err
	}
	qp := quotedprintable.NewWriter(part)
	if _, err := qp.Write([]byte(body)); err != nil {
		return err
	}
	return qp.Close()
}

func randomID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func domainOf(addr string) string {
	if a, err := netmail.ParseAddress(addr); err == nil {
		addr = a.Address
	}
	if i := strings.LastIndexByte(addr, '@'); i >= 0 {
		return addr[i+1:]
	}
	return "localhost"
}
