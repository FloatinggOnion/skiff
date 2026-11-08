package mail

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net"
	netmail "net/mail"
	"strings"
	"testing"
)

// TestLetterIsBrandedAndReadable sends a letter through a relay and reads it back the way a mail client would.
func TestLetterIsBrandedAndReadable(t *testing.T) {
	down := newSink(t)
	host, port, _ := net.SplitHostPort(down.ln.Addr().String())
	relay := &Relay{Host: host, Port: port, From: "alerts@skiff.test"}
	l := Letter{
		Subject: "Ada invited you to Café <Team> on Skiff",
		Heading: "Join <script>alert(1)</script> on Skiff",
		Body:    []string{"First paragraph.", "Second & last."},
		Button:  "Accept invite",
		URL:     "https://dash.skiff.test/invite/abc",
		Note:    "Expires in 7 days.",
		Reason:  "Ignore it if you weren't expecting it.",
	}
	if err := relay.Send(context.Background(), "new@example.com", l); err != nil {
		t.Fatal(err)
	}
	got := down.got()
	if len(got) != 1 || got[0].from != "alerts@skiff.test" || got[0].to[0] != "new@example.com" {
		t.Fatalf("envelope: %+v", got)
	}
	msg, err := netmail.ReadMessage(strings.NewReader(got[0].data))
	if err != nil {
		t.Fatal(err)
	}
	if from := msg.Header.Get("From"); from != `"Skiff" <alerts@skiff.test>` && from != "Skiff <alerts@skiff.test>" {
		t.Fatalf("From = %q", from)
	}
	if subj, _ := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject")); subj != l.Subject {
		t.Fatalf("Subject = %q", subj)
	}
	if !strings.HasSuffix(msg.Header.Get("Message-ID"), "@skiff.test>") {
		t.Fatalf("Message-ID = %q", msg.Header.Get("Message-ID"))
	}

	parts := map[string]string{}
	var walk func(r io.Reader, contentType string)
	walk = func(r io.Reader, contentType string) {
		mt, params, err := mime.ParseMediaType(contentType)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(mt, "multipart/") {
			b, _ := io.ReadAll(r)
			parts[mt] = string(b)
			return
		}
		mr := multipart.NewReader(r, params["boundary"])
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if p.Header.Get("Content-ID") == "<"+markID+">" {
				parts["mark"] = "yes"
			}
			// multipart.Reader decodes quoted-printable itself; base64 stays as sent.
			walk(p, p.Header.Get("Content-Type"))
		}
	}
	walk(msg.Body, msg.Header.Get("Content-Type"))

	html, text := parts["text/html"], parts["text/plain"]
	if parts["mark"] != "yes" || parts["image/png"] == "" {
		t.Fatal("the mark must travel inline with the message")
	}
	if !strings.Contains(html, `src="cid:`+markID+`"`) {
		t.Fatal("the HTML must show the inline mark")
	}
	if strings.Contains(html, "<script>") || !strings.Contains(html, "&lt;script&gt;") {
		t.Fatal("text from the caller must be escaped in the HTML")
	}
	for _, want := range []string{"Second &amp; last.", `href="https://dash.skiff.test/invite/abc"`, "Accept invite", "Expires in 7 days.", "#14403a"} {
		if !strings.Contains(html, want) {
			t.Fatalf("HTML is missing %q", want)
		}
	}
	for _, want := range []string{"Join <script>alert(1)</script> on Skiff", "Second & last.", "Accept invite: https://dash.skiff.test/invite/abc", "Ignore it if you weren't expecting it."} {
		if !strings.Contains(text, want) {
			t.Fatalf("plain text is missing %q:\n%s", want, text)
		}
	}
}
