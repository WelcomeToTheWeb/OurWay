package models

import (
	"time"

	"github.com/google/uuid"
)

// AuditLog is the record of a mutating act: who did it, to what, under which
// client, and what the server answered.
//
// docs/phase1-tasks.md lists "Audit log for user actions" as complete; there
// was no table, no writer and no UI. This is the table, and `store/audit.go` is
// the writer. What the phase-1 shape got wrong is worth stating, because it is
// why this one is written the way it is: an audit log keyed only on the response
// body records intentions, not acts. So the outcome is a **column**, refused and
// failed actions are rows too, and a refused out-of-scope deploy — the single
// most useful event an MSP wants — is a row with `outcome = refused`.
//
// `actor_user_id`, `target_id` and `client_id` are **text**, not `uuid`, and
// Postgres is what makes the reason visible: a `uuid` column rejects the empty
// string, and these columns are legitimately empty. A failed login has no
// authenticated user id; a bulk command refused before any target resolved has
// no entity id; a row about the installation itself has no client. Declaring
// them `uuid` is a schema that cannot store the refusals the ledger exists to
// record — and it fails at migration time on Postgres, not at the row that
// needs it (`TestPostgresMigrationsUpgradeFromV22`).
//
// `client_id` is denormalized on purpose. Scope is rows in `user_clients` and
// devices move between clients; an audit query that joins through the device's
// *current* client would answer "who did this to the fleet I manage today",
// which is not a record — it is a live view wearing a timestamp. The client at
// the time of the act is stored, and a row never changes its tenant.
type AuditLog struct {
	ID string `gorm:"type:uuid;primaryKey" json:"id"`

	// Actor, captured at the moment of the act. The username is stored beside
	// the id because accounts are deleted: an audit row that resolves to
	// "user 5f8… does not exist" is not an audit row.
	ActorUserID   string `gorm:"index;not null;default:''" json:"actor_user_id"`
	ActorUsername string `gorm:"not null;default:''" json:"actor_username"`
	ActorRoles    string `gorm:"not null;default:''" json:"actor_roles"`
	// ActorProvider is where the account came from — `local`, or the name of
	// an identity provider. It is stored for the same reason the username is:
	// providers are deleted, and "who removed a unit" must keep its answer
	// after the directory they signed in through is gone.
	ActorProvider string `gorm:"not null;default:''" json:"actor_provider"`

	// Action is a verb, not a sentence: `device.delete`, `patch.deploy`,
	// `auth.login_failed`. Stable strings, so a filter is a filter.
	Action     string `gorm:"not null;index" json:"action"`
	TargetType string `gorm:"not null;default:''" json:"target_type"`
	TargetID   string `gorm:"not null;default:''" json:"target_id"`
	ClientID   string `gorm:"not null;default:'';index" json:"client_id"`

	// Outcome is what the server answered, and the reason: `ok`,
	// `refused:403`, `refused:404`, `error:500`. A log without the outcome
	// cannot answer the question an audit is read for.
	Outcome string `gorm:"not null;default:''" json:"outcome"`

	// Details is JSON: what changed, and how much of it (a bulk assign of 400
	// units is one row with a count, not 400 rows).
	Details string `gorm:"type:text;not null;default:''" json:"details"`

	SourceIP string `gorm:"not null;default:''" json:"source_ip"`

	CreatedAt time.Time `gorm:"index" json:"created_at"`
}

// TableName pins the table name.
func (AuditLog) TableName() string { return "audit_log" }

// AuditActor is who performed an act, captured at the moment of it. It is a
// plain struct (not a table): the writer copies it onto the row's flat
// `actor_*` columns, so the record stays readable when the account is gone.
type AuditActor struct {
	UserID   string
	Username string
	Roles    string
	Provider string
}

// Audit outcomes. The `refused:*` family is the point of the column: an audit
// log that records only successes is a ledger of what the installation allowed,
// and the interesting events are the ones it did not.
const (
	AuditOutcomeOK       = "ok"
	AuditOutcomeRefused  = "refused"
	AuditOutcomeError    = "error"
	AuditOutcomeNotFound = "not_found"
)

// Audit action names. Declared here so a handler cannot invent a verb that
// reads like two other verbs, and so the coverage test in 2.3-6 can assert
// every mutating route maps to one.
const (
	AuditAuthLogin          = "auth.login"
	AuditAuthLoginFailed    = "auth.login_failed"
	AuditAuthLogout         = "auth.logout"
	AuditAuthPasswordChange = "auth.password_change"
	AuditAuthRegister       = "auth.register"

	AuditDeviceDelete    = "device.delete"
	AuditDeviceReboot    = "device.reboot"
	AuditDeviceStream    = "device.stream"
	AuditDeviceTags      = "device.tags"
	AuditDeviceAssign    = "device.assign"
	AuditMonitoringPurge = "monitoring.purge"

	AuditSessionStart   = "session.start"
	AuditSessionEnd     = "session.end"
	AuditSessionQuality = "session.quality"
	// 2.4-8: keyboard and mouse travel through `POST /api/sessions/:id/input`,
	// which the ledger recorded as `session.start` until now — a control act filed
	// under the act of opening the console. Input gets its own action, because the
	// playback overlay and any reader asking "what did this person do in that
	// session" select on it.
	AuditSessionInput = "session.input"
	// 2.4-8 · recording. Starting a recording is the act that begins capturing a
	// person's screen, so it is a row naming who asked and why the policy allowed
	// it; playing one back and downloading it are the acts that read a person's
	// screen out of a file, so they are rows too.
	AuditSessionRecordStart    = "session.record_start"
	AuditSessionRecordingPlay  = "session.recording_play"
	AuditSessionRecordingRead  = "session.recording_read"
	AuditSessionRecordingFetch = "session.recording_fetch"

	AuditPatchDeploy   = "patch.deploy"
	AuditPatchApprove  = "patch.approve"
	AuditPatchSkip     = "patch.skip"
	AuditPatchScan     = "patch.scan"
	AuditPatchRollback = "patch.rollback"
	AuditPolicyCreate  = "patch.policy_create"
	AuditPolicyUpdate  = "patch.policy_update"
	AuditPolicyDelete  = "patch.policy_delete"

	AuditFilePush   = "file.push"
	AuditFilePull   = "file.pull"
	AuditFileUpload = "file.upload"

	AuditClientCreate = "client.create"
	AuditClientUpdate = "client.update"
	AuditClientDelete = "client.delete"
	AuditClientGrant  = "client.grant"
	// 2.3-4c: the credentials that place a unit inside a tenant. Minting one is
	// the act with no unit, no login and no visible effect to notice it by — the
	// ledger is the only place it leaves a trace, and the trace must not contain
	// the secret.
	AuditClientTokenCreate = "client.token_create"
	AuditClientTokenRevoke = "client.token_revoke"
	AuditUserScopeChange   = "user.scope_change"
	AuditUserRoleChange    = "user.role_change"
	AuditUserCreate        = "user.create"
	AuditUserDelete        = "user.delete"
	// 2.4-3: the two decisions an admin makes about a pending SSO identity — approve
	// it as its own account at the floor, or merge it into the account its asserted
	// email points at.
	AuditUserApprove = "user.approve"
	AuditUserMerge   = "user.merge"
	AuditRoleCreate  = "role.create"
	AuditRoleUpdate  = "role.update"
	AuditRoleDelete  = "role.delete"

	AuditAPIKeyCreate = "api_key.create"
	AuditAPIKeyRotate = "api_key.rotate"
	AuditAPIKeyRevoke = "api_key.revoke"
	AuditAPIKeyUpdate = "api_key.update"
	AuditAPIKeyDelete = "api_key.delete"

	AuditWebhookCreate = "webhook.create"
	AuditWebhookUpdate = "webhook.update"
	AuditWebhookDelete = "webhook.delete"
	AuditWebhookTest   = "webhook.test"

	AuditSSOCreate = "sso.create"
	// 2.4-4: the console edits a provider in place (`PUT /api/sso/providers/:id`),
	// so changing who the installation trusts is a row of its own — the same facts
	// as `sso.create`, and never the client secret.
	AuditSSOUpdate = "sso.update"
	AuditSSOTest   = "sso.test"
	// 2.4-1: asking an IdP what it publishes is a server-side fetch of an
	// operator-supplied URL, so it is recorded like the probe of a saved provider.
	AuditSSODiscover = "sso.discover"
	AuditSSODelete   = "sso.delete"
	// 2.4-3: stamping an SSO identity onto an existing account is the pre-2.4
	// behaviour, reachable only when the operator chose `link_policy=link` for that
	// provider. It gives an existing account's access to a new door, so it is a row.
	AuditSSOLink = "sso.link"

	// 2.4-5: the threshold rules. A rule decides when a unit raises its hand, so
	// the act of changing one is on the record with its scope and numbers.
	AuditThresholdCreate = "alert.threshold_create"
	AuditThresholdUpdate = "alert.threshold_update"
	AuditThresholdDelete = "alert.threshold_delete"

	AuditAlertResolve     = "alert.resolve"
	AuditAlertAcknowledge = "alert.acknowledge"
	AuditAlertAssign      = "alert.assign"
	AuditInstallerCreate  = "installer.create"
	AuditInstallerDelete  = "installer.delete"
)

// NewAuditID mints an audit row id.
func NewAuditID() string { return uuid.NewString() }

// OutcomeCode renders the `outcome` column's value for a status code, so the
// stored outcome is comparable and not a free-text reason.
func AuditOutcomeFor(status int) string {
	switch {
	case status >= 200 && status < 300:
		return AuditOutcomeOK
	case status == 404:
		return AuditOutcomeNotFound
	case status >= 400 && status < 500:
		return AuditOutcomeRefused
	default:
		return AuditOutcomeError
	}
}
