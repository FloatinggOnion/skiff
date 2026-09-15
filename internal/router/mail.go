package router

import (
	"crypto/tls"
	"log"
	"path/filepath"
	"time"

	"github.com/nuelScript/skiff/internal/mail"
)

func (rt *Router) mailRules() mail.Rules {
	rt.mu.Lock()
	defer rt.mu.Unlock()
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

// startMail serves inbound SMTP for forwarded addresses; a failure only logs, so mail can never take down web routing.
func (rt *Router) startMail(certs func(*tls.ClientHelloInfo) (*tls.Certificate, error)) {
	if rt.SMTPAddr == "" || rt.MailFile == "" {
		return
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
	}
	go func() {
		if err := srv.ListenAndServe(rt.SMTPAddr); err != nil {
			log.Printf("router smtp %s listener stopped: %v", rt.SMTPAddr, err)
		}
	}()
}

func (rt *Router) MailHost() string {
	if len(rt.Domains) == 0 {
		return "localhost"
	}
	return "mx." + rt.Domains[0]
}
