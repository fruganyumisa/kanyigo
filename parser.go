package main

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type LogEntry struct {
	TSUTC        time.Time `json:"ts_utc"`
	Host         string    `json:"host"`
	Process      string    `json:"process"`
	QueueID      string    `json:"queue_id"`
	MailFrom     string    `json:"mail_from"`
	MailTo       string    `json:"mail_to"`
	Status       string    `json:"status"`
	Relay        string    `json:"relay"`
	Delay        *float64  `json:"delay,omitempty"`
	Delays       string    `json:"delays"`
	DSN          string    `json:"dsn"`
	MessageID    string    `json:"message_id"`
	SizeBytes    *int64    `json:"size_bytes,omitempty"`
	QueuedAs     string    `json:"queued_as"`
	MailID       string    `json:"mail_id"`
	Subject      string    `json:"subject"`
	Hits         *float64  `json:"hits,omitempty"`
	Helo         string    `json:"helo"`
	AmavisOrigin string    `json:"amavis_origin"`
	IsJunk       bool      `json:"is_junk"`
	SpamScore    *float64  `json:"spam_score,omitempty"`
	Raw          string    `json:"raw"`
	RawHash      string    `json:"raw_hash"`
}

// Example postfix lines:
// Jan 23 19:27:17 mail postfix/qmgr[1234]: 1234ABCD: from=<a@b.com>, size=1234, nrcpt=1 (queue active)
// Jan 23 19:27:18 mail postfix/smtp[1235]: 1234ABCD: to=<c@d.com>, relay=mx.example[1.2.3.4]:25, delay=0.1, delays=0.01/0/0.02/0.07, dsn=2.0.0, status=sent (250 Ok)

var (
	reSyslog         = regexp.MustCompile(`^([A-Z][a-z]{2})\s+([ 0-9]{2})\s+([0-9]{2}:[0-9]{2}:[0-9]{2})\s+(\S+)\s+([^:]+):\s+(.*)$`)
	reRFC3339        = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T[0-9:.]+(?:Z|[+-][0-9:]+))\s+(\S+)\s+([^:]+):\s+(.*)$`)
	reQueue          = regexp.MustCompile(`^([A-Za-z0-9]+):\s+(.*)$`)
	reKV             = regexp.MustCompile(`(\w+)=<([^>]*)>|(\w+)=([^,\s]+)`)
	reAmavisFrom     = regexp.MustCompile(`(?i)\bFrom:\s*<([^>]+)>`)
	reAmavisArrow    = regexp.MustCompile(`(?i)>\s*->\s*<([^>]+)>`)
	reAmavisQueueID  = regexp.MustCompile(`(?i)\bQueue-ID:\s*([A-Za-z0-9]+)`)
	reAmavisStatus   = regexp.MustCompile(`(?i)\b(Passed|Blocked|Rejected)\s+([A-Z]+)`)
	reAmavisQueuedAs = regexp.MustCompile(`(?i)\bqueued_as:\s*([A-Za-z0-9]+)`)
	reAmavisMailID   = regexp.MustCompile(`(?i)\bmail_id:\s*([A-Za-z0-9_]+)`)
	reAmavisSubject  = regexp.MustCompile(`(?i)\bSubject:\s*\"([^\"]*)\"`)
	reAmavisHits     = regexp.MustCompile(`(?i)\bHits:\s*([-0-9.]+)`)
	reAmavisHelo     = regexp.MustCompile(`(?i)\bhelo=([^\s,]+)`)
	reAmavisOrigin   = regexp.MustCompile(`(?i)\{(RelayedInbound|RelayedInternal|RelayedOutbound)\}`)
	reDovecotQueueID = regexp.MustCompile(`(?i)\bQueue-ID:\s*([A-Za-z0-9]+)`)
	reDovecotLMTP    = regexp.MustCompile(`(?i)\bdovecot\b.*\blmtp(?:\([^)]*\))?`)
	reDovecotJunk    = regexp.MustCompile(`(?i)\b(?:saved|stored|delivered)\b.*\b(?:Junk|Spam)\b`)
)

func ParseLine(line string, now time.Time, loc *time.Location) (*LogEntry, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, false
	}
	var (
		ts      time.Time
		host    string
		process string
		msg     string
	)

	if m := reRFC3339.FindStringSubmatch(line); m != nil {
		parsed, err := time.Parse(time.RFC3339Nano, m[1])
		if err != nil {
			return nil, false
		}
		ts = parsed.UTC()
		host, process, msg = m[2], m[3], m[4]
	} else if m := reSyslog.FindStringSubmatch(line); m != nil {
		monthStr, dayStr, timeStr := m[1], strings.TrimSpace(m[2]), m[3]
		host, process, msg = m[4], m[5], m[6]
		day, err := strconv.Atoi(dayStr)
		if err != nil {
			return nil, false
		}
		month, err := time.Parse("Jan", monthStr)
		if err != nil {
			return nil, false
		}
		parsedTime, err := time.Parse("15:04:05", timeStr)
		if err != nil {
			return nil, false
		}
		ts = inferLegacyTimestamp(now, loc, month.Month(), day, parsedTime).UTC()
	} else {
		return nil, false
	}

	queueID := ""
	payload := msg
	if qm := reQueue.FindStringSubmatch(msg); qm != nil {
		queueID = qm[1]
		payload = qm[2]
	}

	entry := &LogEntry{
		TSUTC:   ts,
		Host:    host,
		Process: process,
		QueueID: queueID,
		Raw:     line,
	}

	for _, kv := range reKV.FindAllStringSubmatch(payload, -1) {
		key := ""
		val := ""
		if kv[1] != "" {
			key = kv[1]
			val = kv[2]
		} else {
			key = kv[3]
			val = kv[4]
		}
		switch key {
		case "from":
			entry.MailFrom = strings.TrimSpace(val)
		case "to":
			entry.MailTo = strings.TrimSpace(val)
		case "status":
			entry.Status = strings.TrimSpace(val)
		case "relay":
			entry.Relay = strings.TrimSpace(val)
		case "delay":
			if d, err := strconv.ParseFloat(val, 64); err == nil {
				entry.Delay = &d
			}
		case "delays":
			entry.Delays = strings.TrimSpace(val)
		case "dsn":
			entry.DSN = strings.TrimSpace(val)
		case "message-id":
			entry.MessageID = strings.Trim(strings.TrimSpace(val), "<>")
		case "size":
			if s, err := strconv.ParseInt(val, 10, 64); err == nil {
				entry.SizeBytes = &s
			}
		}
	}

	if entry.MailFrom == "" {
		if m := reAmavisFrom.FindStringSubmatch(payload); m != nil {
			entry.MailFrom = strings.TrimSpace(m[1])
		}
	}
	if entry.MailTo == "" {
		if m := reAmavisArrow.FindStringSubmatch(payload); m != nil {
			entry.MailTo = strings.TrimSpace(m[1])
		}
	}
	if entry.QueueID == "" {
		if m := reAmavisQueueID.FindStringSubmatch(payload); m != nil {
			entry.QueueID = strings.TrimSpace(m[1])
		}
	}
	if entry.Status == "" {
		if m := reAmavisStatus.FindStringSubmatch(payload); m != nil {
			entry.Status = strings.ToLower(strings.TrimSpace(m[1] + " " + m[2]))
		}
	}
	if entry.QueuedAs == "" {
		if m := reAmavisQueuedAs.FindStringSubmatch(payload); m != nil {
			entry.QueuedAs = strings.TrimSpace(m[1])
		}
	}
	if entry.MailID == "" {
		if m := reAmavisMailID.FindStringSubmatch(payload); m != nil {
			entry.MailID = strings.TrimSpace(m[1])
		}
	}
	if entry.Subject == "" {
		if m := reAmavisSubject.FindStringSubmatch(payload); m != nil {
			entry.Subject = strings.TrimSpace(m[1])
		}
	}
	if entry.Hits == nil {
		if m := reAmavisHits.FindStringSubmatch(payload); m != nil {
			if v, err := strconv.ParseFloat(m[1], 64); err == nil {
				entry.Hits = &v
			}
		}
	}
	if entry.Helo == "" {
		if m := reAmavisHelo.FindStringSubmatch(payload); m != nil {
			entry.Helo = strings.TrimSpace(m[1])
		}
	}
	if entry.AmavisOrigin == "" {
		if m := reAmavisOrigin.FindStringSubmatch(payload); m != nil {
			entry.AmavisOrigin = strings.TrimSpace(m[1])
		}
	}
	if strings.HasPrefix(strings.ToLower(process), "amavis[") {
		lowerStatus := strings.ToLower(entry.Status)
		if lowerStatus == "passed spam" || lowerStatus == "passed spammy" {
			entry.IsJunk = true
		}
		if entry.Hits != nil {
			score := *entry.Hits
			entry.SpamScore = &score
		}
	}
	if reDovecotLMTP.MatchString(process + " " + payload) {
		if entry.QueueID == "" {
			if m := reDovecotQueueID.FindStringSubmatch(payload); m != nil {
				entry.QueueID = strings.TrimSpace(m[1])
			}
		}
		if reDovecotJunk.MatchString(payload) {
			entry.IsJunk = true
		}
	}

	h := sha256.Sum256([]byte(line))
	entry.RawHash = hex.EncodeToString(h[:])
	return entry, true
}

func inferLegacyTimestamp(now time.Time, loc *time.Location, month time.Month, day int, clock time.Time) time.Time {
	localNow := now.In(loc)
	best := time.Date(localNow.Year(), month, day, clock.Hour(), clock.Minute(), clock.Second(), 0, loc)
	bestDistance := absDuration(best.Sub(localNow))
	for _, year := range []int{localNow.Year() - 1, localNow.Year() + 1} {
		candidate := time.Date(year, month, day, clock.Hour(), clock.Minute(), clock.Second(), 0, loc)
		if distance := absDuration(candidate.Sub(localNow)); distance < bestDistance {
			best = candidate
			bestDistance = distance
		}
	}
	return best
}

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}
