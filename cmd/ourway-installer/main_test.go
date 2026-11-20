package main

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

// The enrollment credential is a header, and the header's name is a contract with
// the server (`resolveEnrollment` in server/api/devices.go reads exactly this
// key). A typo in it is invisible from the installer's own output: the request
// still goes out, the server still answers 401, and the operator sees only a
// refusal. So the header is asserted, not assumed.

func TestRegisterDeviceSendsTheEnrollmentCredential(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Enrollment-Secret")
		if r.URL.Path != "/api/agent/register" {
			t.Errorf("enrollment posted to %s, want /api/agent/register", r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"device_key":"dk_123"}`))
	}))
	defer srv.Close()

	oldServer, oldToken := *flagServer, *flagToken
	t.Cleanup(func() { *flagServer, *flagToken = oldServer, oldToken })

	*flagServer = srv.URL
	*flagToken = "owt_testtoken"

	key, err := registerDevice("windows", "amd64")
	if err != nil {
		t.Fatalf("enrollment failed against a server that accepted it: %v", err)
	}
	if key != "dk_123" {
		t.Errorf("device key = %q, want dk_123", key)
	}
	if gotHeader != "owt_testtoken" {
		t.Errorf("X-Enrollment-Secret = %q, want the token passed to --token", gotHeader)
	}
}

// Without --token nothing is sent, rather than an empty header: the server treats
// "absent" as "installment is open", and a present-but-empty one is a different
// answer. The point of asserting it is the error text: a 401 with no credential
// in play is nearly always a missing --token, and the operator has to be told.
func TestRegisterDeviceWithoutATokenSendsNoCredentialAndSaysSo(t *testing.T) {
	var headerPresent bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, headerPresent = r.Header["X-Enrollment-Secret"]
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"enrollment requires the enrollment secret"}`))
	}))
	defer srv.Close()

	oldServer, oldToken := *flagServer, *flagToken
	t.Cleanup(func() { *flagServer, *flagToken = oldServer, oldToken })

	*flagServer = srv.URL
	*flagToken = ""

	_, err := registerDevice("linux", "arm64")
	if err == nil {
		t.Fatal("a 401 from the server was reported as success")
	}
	if headerPresent {
		t.Error("an X-Enrollment-Secret header was sent when --token was empty")
	}
	if !strings.Contains(err.Error(), "--token") {
		t.Errorf("the failure does not mention --token, so the operator is left guessing: %v", err)
	}
}

// The server can accept an enrollment and still not hand back a key — a proxy
// that rewrites the body, an older server, a partial deploy. Treating that as a
// success is how an agent with an empty key gets installed and then looks like an
// offline machine forever.
func TestRegisterDeviceFailsWhenNoKeyComesBack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	oldServer := *flagServer
	t.Cleanup(func() { *flagServer = oldServer })
	*flagServer = srv.URL

	if _, err := registerDevice("linux", "amd64"); err == nil {
		t.Fatal("an enrollment reply with no device_key was reported as success")
	}
}

// A failed step must be remembered, because the summary's whole job is to be
// true. This is the regression the change is about: the failure used to be a
// warning that vanished into the console scroll while the last line read
// "Installation complete!".
func TestRunRecordsAFailedStep(t *testing.T) {
	old := stepFailures
	t.Cleanup(func() { stepFailures = old })
	stepFailures = nil

	if err := run("ourway-installer-does-not-exist", "--version"); err == nil {
		t.Fatal("run reported success for a program that does not exist")
	}
	if len(stepFailures) != 1 {
		t.Fatalf("a failed step recorded %d failures, want 1: %v", len(stepFailures), stepFailures)
	}
	if !strings.Contains(stepFailures[0], "ourway-installer-does-not-exist") {
		t.Errorf("the record does not name what failed, so the summary is useless: %q", stepFailures[0])
	}

	// And a step that succeeds must not be recorded. `true` is chosen per
	// platform rather than shelling out to a shell, so the assertion does not
	// quietly depend on what the runner happens to have installed.
	if runtime.GOOS == "windows" {
		if err := run("cmd", "/c", "exit 0"); err != nil {
			t.Errorf("run reported failure for a step that succeeded: %v", err)
		}
	} else {
		if err := run("/bin/true"); err != nil {
			t.Errorf("run reported failure for a step that succeeded: %v", err)
		}
	}
	if len(stepFailures) != 1 {
		t.Errorf("a successful step added a failure: %v", stepFailures)
	}
}
