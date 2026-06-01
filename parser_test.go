package main

import (
	"testing"
	"time"
)

func TestParseLine(t *testing.T) {
	line := "2026-01-23T19:27:18.123456+03:00 mail postfix/smtp[1235]: 1234ABCD: to=<c@d.com>, relay=mx.example[1.2.3.4]:25, delay=0.1, delays=0.01/0/0.02/0.07, dsn=2.0.0, status=sent (250 Ok)"
	now := time.Date(2026, 1, 23, 20, 0, 0, 0, time.UTC)
	entry, ok := ParseLine(line, now, time.UTC)
	if !ok {
		t.Fatalf("expected ok")
	}
	if entry.QueueID != "1234ABCD" {
		t.Fatalf("queue id: %s", entry.QueueID)
	}
	if entry.MailTo != "c@d.com" {
		t.Fatalf("mail to: %s", entry.MailTo)
	}
	if entry.Status != "sent" {
		t.Fatalf("status: %s", entry.Status)
	}
	if entry.Delay == nil || *entry.Delay != 0.1 {
		t.Fatalf("delay: %+v", entry.Delay)
	}
}

func TestParseLineAmavis(t *testing.T) {
	line := "2026-01-26T17:32:51.883442+03:00 mail amavis[391814]: (391814-12) Passed CLEAN {RelayedInbound}, [52.101.85.41]:61992 [52.101.85.41] ESMTP/ESMTP <sarah.ituwe@msc.com> -> <sesmail@kkcompany.co.tz>, (ESMTPS://[52.101.85.41]:61992), Queue-ID: 4f09xB23GVzCtLh, Message-ID: <AMAP104MB0151A71868CA906E4BE21D41F593A@amap104mb0151.namp104.prod.outlook.com>, mail_id: y15IxYcZdce1, b: Cme0jBqe1, Hits: -5.416, size: 192050, queued_as: 4f09xH5zDgzCtND, Subject: \"Re: DEMURRAGE\", From: <sarah.ituwe@msc.com> (dkim:AUTHOR), helo=BYAPR05CU005.outbound.protection.outlook.com"
	now := time.Date(2026, 1, 26, 18, 0, 0, 0, time.UTC)
	entry, ok := ParseLine(line, now, time.UTC)
	if !ok {
		t.Fatalf("expected ok")
	}
	if entry.MailFrom != "sarah.ituwe@msc.com" {
		t.Fatalf("mail from: %s", entry.MailFrom)
	}
	if entry.MailTo != "sesmail@kkcompany.co.tz" {
		t.Fatalf("mail to: %s", entry.MailTo)
	}
	if entry.QueueID != "4f09xB23GVzCtLh" {
		t.Fatalf("queue id: %s", entry.QueueID)
	}
	if entry.Status != "passed clean" {
		t.Fatalf("status: %s", entry.Status)
	}
	if entry.QueuedAs != "4f09xH5zDgzCtND" {
		t.Fatalf("queued_as: %s", entry.QueuedAs)
	}
	if entry.MailID != "y15IxYcZdce1" {
		t.Fatalf("mail_id: %s", entry.MailID)
	}
	if entry.Subject != "Re: DEMURRAGE" {
		t.Fatalf("subject: %s", entry.Subject)
	}
	if entry.Hits == nil || *entry.Hits != -5.416 {
		t.Fatalf("hits: %+v", entry.Hits)
	}
	if entry.Helo != "BYAPR05CU005.outbound.protection.outlook.com" {
		t.Fatalf("helo: %s", entry.Helo)
	}
	if entry.AmavisOrigin != "RelayedInbound" {
		t.Fatalf("origin: %s", entry.AmavisOrigin)
	}
}

func TestParseLineAmavisSpammy(t *testing.T) {
	line := "Jun  1 10:15:42 mail amavis[1234]: (01234-01) Passed SPAMMY {RelayedInbound}, <sender@example.com> -> <user@example.com>, Queue-ID: 4YgX9z0zKzHz, Hits: 8.75, queued_as: 4YgX9z0zKzJz"
	entry, ok := ParseLine(line, time.Date(2026, 6, 1, 11, 0, 0, 0, time.UTC), time.UTC)
	if !ok {
		t.Fatal("expected spammy Amavis line to parse")
	}
	if !entry.IsJunk {
		t.Fatal("expected spammy Amavis line to be junk")
	}
	if entry.SpamScore == nil || *entry.SpamScore != 8.75 {
		t.Fatalf("spam score: %+v", entry.SpamScore)
	}
}

func TestParseLineDovecotLMTPJunk(t *testing.T) {
	line := "Jun  1 10:15:43 mail dovecot: lmtp(user@example.com)<1234><abc>: sieve: msgid=<test@example.com>: saved mail to Junk; Queue-ID: 4YgX9z0zKzHz"
	entry, ok := ParseLine(line, time.Date(2026, 6, 1, 11, 0, 0, 0, time.UTC), time.UTC)
	if !ok {
		t.Fatal("expected Dovecot LMTP line to parse")
	}
	if entry.QueueID != "4YgX9z0zKzHz" {
		t.Fatalf("queue id: %s", entry.QueueID)
	}
	if !entry.IsJunk {
		t.Fatal("expected Dovecot Junk delivery to be classified as junk")
	}
}
