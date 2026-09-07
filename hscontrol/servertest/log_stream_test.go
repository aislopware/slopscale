package servertest_test

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/logstream"
	"github.com/juanfont/headscale/hscontrol/servertest"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// logSink is an HTTP server that keeps the entries it receives, as the
// generic JSON destination sends them.
type logSink struct {
	*httptest.Server

	mu      sync.Mutex
	entries []logstream.Entry
	auth    string
}

func newLogSink(t *testing.T) *logSink {
	t.Helper()

	s := &logSink{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}

		var batch []logstream.Entry

		err = json.Unmarshal(body, &batch)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}

		s.mu.Lock()
		s.entries = append(s.entries, batch...)
		s.auth = req.Header.Get("Authorization")
		s.mu.Unlock()

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(s.Close)

	return s
}

func (s *logSink) find(action string) *logstream.Entry {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.entries {
		if s.entries[i].Action == action {
			return &s.entries[i]
		}
	}

	return nil
}

func (s *logSink) waitFor(t *testing.T, action string) logstream.Entry {
	t.Helper()

	var found logstream.Entry

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		e := s.find(action)
		if assert.NotNil(c, e, "no %q entry yet", action) {
			found = *e
		}
	}, 10*time.Second, 20*time.Millisecond)

	return found
}

// TestLogStreaming proves that an audit event recorded through the API
// reaches a registered sink with the actor and target filled in, that
// the token travels as the destination expects and is never listed, that
// a disabled stream ships nothing, and that the test endpoint reports
// the sink's answer.
//
//nolint:tparallel // later steps depend on the state earlier ones leave behind
func TestLogStreaming(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "stream-owner")
	ownerKey := srv.CreateAPIKey(t, owner)

	sink := newLogSink(t)

	var streamID string

	t.Run("a stream is validated on the way in", func(t *testing.T) {
		for name, body := range map[string]map[string]any{
			"bad url":      {"name": "x", "destination": "http", "url": "ftp://x"},
			"no token":     {"name": "x", "destination": "splunk", "url": "https://splunk.example/hec"},
			"unknown sink": {"name": "x", "destination": "s3", "url": "https://x"},
			"blank name":   {"name": " ", "destination": "http", "url": "https://x"},
			"missing url":  {"name": "x", "destination": "http"},
		} {
			status, resp := apiCall(t, client, ownerKey, http.MethodPost, v1+"/log-stream", body)
			assert.Contains(
				t,
				[]int{http.StatusBadRequest, http.StatusUnprocessableEntity},
				status,
				"%s: %v",
				name,
				resp,
			)
		}
	})

	t.Run("an audit event reaches the sink", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/log-stream", map[string]any{
			"name": "siem", "destination": "http", "url": sink.URL, "token": "s3cret",
		})
		require.Equal(t, http.StatusOK, status, body)

		id, ok := field(t, body, "logStream", "id").(string)
		require.True(t, ok)

		streamID = id

		assert.Equal(t, true, field(t, body, "logStream", "hasToken"))
		stream, ok := body["logStream"].(map[string]any)
		require.True(t, ok)
		assert.Nil(t, stream["token"], "the token is never returned")

		// Creating the stream is itself audited and shipped, as is
		// everything after it.
		created := sink.waitFor(t, "logstream.create")
		assert.Equal(t, "stream-owner", created.Actor.Name)
		assert.Equal(t, logstream.LogType, created.Type)
		require.NotNil(t, created.Target)
		assert.Equal(t, "logstream", created.Target.Kind)
		assert.Equal(t, "siem", created.Target.Name)
		assert.Equal(t, http.StatusOK, created.Outcome)

		sink.mu.Lock()
		assert.Equal(t, "Bearer s3cret", sink.auth)
		sink.mu.Unlock()

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/user", map[string]any{"name": "streamed"})
		require.Equal(t, http.StatusOK, status, body)

		entry := sink.waitFor(t, "user.create")
		assert.Equal(t, "streamed", entry.Target.Name)
		assert.NotZero(t, entry.ID, "the audit log ID travels with the entry")
	})

	t.Run("the list hides the token and shows the counters", func(t *testing.T) {
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/log-stream", nil)
			require.Equal(c, http.StatusOK, status, body)

			streams, ok := field(t, body, "logStreams").([]any)
			require.True(c, ok)
			require.Len(c, streams, 1)

			stream, ok := streams[0].(map[string]any)
			require.True(c, ok)
			assert.Nil(c, stream["token"])
			assert.Equal(c, "200", stream["lastDeliveryStatus"])
			assert.GreaterOrEqual(c, stream["delivered"], float64(2))
		}, 10*time.Second, 20*time.Millisecond)
	})

	t.Run("an update without a token keeps it, and disabling stops shipping", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPut, v1+"/log-stream/"+streamID, map[string]any{
			"name": "siem", "destination": "http", "url": sink.URL, "enabled": false,
		})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, field(t, body, "logStream", "hasToken"))
		assert.Equal(t, false, field(t, body, "logStream", "enabled"))

		// The update was recorded before the reload disabled the
		// stream, so it may or may not arrive; what follows must not.
		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/user", map[string]any{"name": "unstreamed"})
		require.Equal(t, http.StatusOK, status, body)

		// Give a wrongly shipped batch time to land before looking.
		require.Never(t, func() bool {
			sink.mu.Lock()
			defer sink.mu.Unlock()

			for _, e := range sink.entries {
				if e.Action == "user.create" && e.Target != nil && e.Target.Name == "unstreamed" {
					return true
				}
			}

			return false
		}, 300*time.Millisecond, 20*time.Millisecond, "a disabled stream ships nothing")
	})

	t.Run("the test endpoint reports the sink's answer", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/log-stream/"+streamID+"/test", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, field(t, body, "delivered"))
		assert.NotNil(t, sink.find("logstream.test"))

		rejecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		t.Cleanup(rejecting.Close)

		status, body = apiCall(t, client, ownerKey, http.MethodPut, v1+"/log-stream/"+streamID, map[string]any{
			"name": "siem", "destination": "datadog", "url": rejecting.URL, "token": "bad",
		})
		require.Equal(t, http.StatusOK, status, body)

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/log-stream/"+streamID+"/test", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, false, field(t, body, "delivered"))
		assert.Equal(t, "401", field(t, body, "status"))
	})

	t.Run("delete", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodDelete, v1+"/log-stream/"+streamID, nil)
		require.Equal(t, http.StatusOK, status, body)

		status, _ = apiCall(t, client, ownerKey, http.MethodGet, v1+"/log-stream/"+streamID, nil)
		assert.Equal(t, http.StatusNotFound, status)
	})
}

// TestLogStreamRoles checks who may manage streams: both admin roles
// write, an auditor reads, a member sees nothing.
func TestLogStreamRoles(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "stream-roles-owner")
	ownerKey := srv.CreateAPIKey(t, owner)

	keys := map[types.Role]string{}

	for _, role := range []types.Role{types.RoleNetworkAdmin, types.RoleITAdmin, types.RoleAuditor, types.RoleMember} {
		user := srv.CreateUser(t, "stream-roles-"+string(role))
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/user/"+userID(user)+"/role",
			map[string]string{"role": string(role)})
		require.Equal(t, http.StatusOK, status, body)

		keys[role] = srv.CreateAPIKey(t, user)
	}

	create := map[string]any{"name": "x", "destination": "http", "url": "https://sink.example/logs"}

	for _, role := range []types.Role{types.RoleNetworkAdmin, types.RoleITAdmin} {
		status, body := apiCall(t, client, keys[role], http.MethodPost, v1+"/log-stream", create)
		assert.Equal(t, http.StatusOK, status, "%s creates: %v", role, body)
	}

	status, body := apiCall(t, client, keys[types.RoleAuditor], http.MethodGet, v1+"/log-stream", nil)
	assert.Equal(t, http.StatusOK, status, body)
	assert.Len(t, body["logStreams"], 2)

	status, body = apiCall(t, client, keys[types.RoleAuditor], http.MethodPost, v1+"/log-stream", create)
	assert.Equal(t, http.StatusForbidden, status, body)

	status, body = apiCall(t, client, keys[types.RoleMember], http.MethodGet, v1+"/log-stream", nil)
	assert.Equal(t, http.StatusForbidden, status, body)
}

// smtpServer is the least of SMTP: it greets, takes one message per
// connection in the clear and keeps it.
type smtpServer struct {
	addr     string
	mu       sync.Mutex
	messages []smtpMessage
}

type smtpMessage struct {
	from string
	to   []string
	data string
}

func newSMTPServer(t *testing.T) *smtpServer {
	t.Helper()

	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })

	s := &smtpServer{addr: ln.Addr().String()}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}

			go s.serve(conn)
		}
	}()

	return s
}

func (s *smtpServer) serve(conn net.Conn) {
	defer conn.Close()

	r := bufio.NewReader(conn)
	reply := func(line string) { _, _ = io.WriteString(conn, line+"\r\n") }

	reply("220 test ESMTP")

	var msg smtpMessage

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}

		line = strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(line)

		switch {
		case strings.HasPrefix(upper, "EHLO"):
			reply("250-test")
			reply("250 8BITMIME")
		case strings.HasPrefix(upper, "HELO"):
			reply("250 test")
		case strings.HasPrefix(upper, "MAIL FROM:"):
			msg.from = address(line[len("MAIL FROM:"):])

			reply("250 ok")
		case strings.HasPrefix(upper, "RCPT TO:"):
			msg.to = append(msg.to, address(line[len("RCPT TO:"):]))

			reply("250 ok")
		case upper == "DATA":
			reply("354 go ahead")

			var data strings.Builder

			for {
				l, err := r.ReadString('\n')
				if err != nil || l == ".\r\n" {
					break
				}

				data.WriteString(l)
			}

			msg.data = data.String()

			s.mu.Lock()
			s.messages = append(s.messages, msg)
			s.mu.Unlock()

			reply("250 queued")
		case upper == "QUIT":
			reply("221 bye")

			return
		default:
			reply("250 ok")
		}
	}
}

// address is the path of a MAIL or RCPT argument without the brackets
// and the parameters that may follow.
func address(arg string) string {
	arg = strings.TrimSpace(arg)
	if start := strings.IndexByte(arg, '<'); start >= 0 {
		if end := strings.IndexByte(arg, '>'); end > start {
			return arg[start+1 : end]
		}
	}

	return strings.Fields(arg)[0]
}

func (s *smtpServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.messages)
}

// TestNotificationProviders proves that a Telegram endpoint gets the
// chat in the body with the URL stripped of it, that an email endpoint
// is refused without a mail server and delivered through one, and that
// the validation catches the URL shapes each provider needs.
func TestNotificationProviders(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		telegram map[string]any
		path     string
	)

	bot := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)

		mu.Lock()
		_ = json.Unmarshal(body, &telegram)
		path = req.URL.RequestURI()
		mu.Unlock()

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(bot.Close)

	t.Run("without a mail server", func(t *testing.T) {
		t.Parallel()

		srv := servertest.NewServer(t)
		client := srv.HTTPClient(t)
		v1 := srv.URL + "/api/v1"

		owner := srv.CreateUser(t, "notify-owner")
		ownerKey := srv.CreateAPIKey(t, owner)

		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/webhook", map[string]any{
			"url": "mailto:ops@example.com", "providerType": "email", "subscriptions": []string{"nodeCreated"},
		})
		assert.Equal(t, http.StatusBadRequest, status, "email needs notifications.smtp: %v", body)

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/webhook", map[string]any{
			"url": bot.URL + "/bot123/sendMessage", "providerType": "telegram", "subscriptions": []string{"test"},
		})
		assert.Equal(t, http.StatusBadRequest, status, "telegram needs chat_id: %v", body)

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/webhook", map[string]any{
			"url":           bot.URL + "/bot123/sendMessage?chat_id=-42",
			"providerType":  "telegram",
			"subscriptions": []string{"nodeCreated"},
		})
		require.Equal(t, http.StatusOK, status, body)

		id, ok := field(t, body, "webhook", "id").(string)
		require.True(t, ok)

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/webhook/"+id+"/test", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, field(t, body, "delivered"))

		mu.Lock()
		defer mu.Unlock()

		assert.Equal(t, "-42", telegram["chat_id"])
		assert.Contains(t, telegram["text"], "test event")
		assert.Equal(t, "/bot123/sendMessage", path, "the chat left the URL")
	})

	t.Run("with a mail server", func(t *testing.T) {
		t.Parallel()

		mail := newSMTPServer(t)
		host, port, err := net.SplitHostPort(mail.addr)
		require.NoError(t, err)

		portNum, err := strconv.Atoi(port)
		require.NoError(t, err)

		srv := servertest.NewServer(t, servertest.WithSMTP(types.SMTPConfig{
			Host: host, Port: portNum, From: "Headscale <hs@example.com>", Encryption: types.SMTPNoEncryption,
		}))
		client := srv.HTTPClient(t)
		v1 := srv.URL + "/api/v1"

		owner := srv.CreateUser(t, "mail-owner")
		ownerKey := srv.CreateAPIKey(t, owner)

		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/webhook", map[string]any{
			"url": "mailto:not an address", "providerType": "email", "subscriptions": []string{"nodeCreated"},
		})
		assert.Equal(t, http.StatusBadRequest, status, body)

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/webhook", map[string]any{
			"url":           "mailto:ops@example.com, sec@example.com",
			"providerType":  "email",
			"subscriptions": []string{"nodeCreated"},
		})
		require.Equal(t, http.StatusOK, status, body)

		id, ok := field(t, body, "webhook", "id").(string)
		require.True(t, ok)

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/webhook/"+id+"/test", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, field(t, body, "delivered"), body)
		assert.Equal(t, "sent", field(t, body, "status"))

		require.Equal(t, 1, mail.count())

		mail.mu.Lock()
		defer mail.mu.Unlock()

		assert.Equal(t, "hs@example.com", mail.messages[0].from)
		assert.Equal(t, []string{"ops@example.com", "sec@example.com"}, mail.messages[0].to)
		assert.Contains(t, mail.messages[0].data, "Subject: ")
		assert.Contains(t, mail.messages[0].data, "This is a test event from headscale.")
	})
}
