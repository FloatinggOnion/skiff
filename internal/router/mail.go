package router

import (
	"crypto/tls"
	"log"
	"net"
	"path/filepath"
	"time"

	"github.com/nuelScript/skiff/internal/mail"
)

func (rt *Router) mailRules() mail.Rules {
	rt.mailMu.Lock()
	defer rt.mailMu.Unlock()
	if rt.cachedRules != nil && time.Since(rt.cachedRulesAt) < 2*time.Second {
		return rt.cachedRules
	}
	if rules, err := mail.LoadRules(rt.MailFile); err == nil {
		rt.cachedRules = rules
	} else if rt.cachedRules == nil {
		rt.cachedRules = mail.Rules{}
	}
	rt.cachedRulesAt = time.Now()
	return rt.cachedRules
}

// startMail serves inbound SMTP for forwarded addresses, waiting until forwarding is actually configured so an instance that doesn't use it never opens :25. Failures only log — mail must never take down web routing.
func (rt *Router) startMail(certs func(*tls.ClientHelloInfo) (*tls.Certificate, error)) {
	if rt.SMTPAddr == "" || rt.MailFile == "" {
		return
	}
	go func() {
		for len(rt.mailRules()) == 0 {
			time.Sleep(30 * time.Second)
		}
		key, err := mail.LoadKey(filepath.Join(filepath.Dir(rt.MailFile), "srs.key"))
		if err != nil {
			log.Printf("router smtp disabled: %v", err)
			return
		}
		host := rt.MailHost()
		srv := &mail.Server{
			Hostname: host,
			Rules:    rt.mailRules,
			Logf:     log.Printf,
			Forwarder: &mail.Forwarder{
				Hostname: host,
				SRS:      mail.SRS{Key: key},
				Relay:    mail.RelayFromEnv(),
				Logf:     log.Printf,
			},
		}
		if certs != nil {
			srv.TLSConfig = &tls.Config{
				MinVersion: tls.VersionTLS12,
				// Sending MTAs often omit SNI; answer with the MX hostname's certificate.
				GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
					if hello.ServerName == "" {
						h := *hello
						h.ServerName = host
						return certs(&h)
					}
					return certs(hello)
				},
			}
			go warmCert(host)
		}
		log.Printf("router smtp: receiving on %s as %s", rt.SMTPAddr, host)
		if err := srv.ListenAndServe(rt.SMTPAddr); err != nil {
			log.Printf("router smtp %s listener stopped: %v", rt.SMTPAddr, err)
		}
	}()
}

// warmCert handshakes against our own edge so the MX hostname's certificate is issued before a sender's first STARTTLS has to wait on it.
func warmCert(host string) {
	for range 5 {
		time.Sleep(2 * time.Second)
		// Dialing our own listener; the point is to trigger issuance for host, not to verify it.
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 60 * time.Second}, "tcp", "127.0.0.1:443",
			&tls.Config{ServerName: host, InsecureSkipVerify: true})
		if err == nil {
			_ = conn.Close()
			return
		}
	}
}

func (rt *Router) MailHost() string {
	if len(rt.Domains) == 0 {
		return "localhost"
	}
	return "mx." + rt.Domains[0]
}
