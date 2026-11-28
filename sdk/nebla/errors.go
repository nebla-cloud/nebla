// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla

import (
	"errors"
	"fmt"
)

// ErrorCode is the machine-readable code of an API error.
type ErrorCode string

// Error codes the API emits.
const (
	CodeAlreadyMember              ErrorCode = "already_member"
	CodeAlreadySetup               ErrorCode = "already_setup"
	CodeBackupBusy                 ErrorCode = "backup_busy"
	CodeBackupInProgress           ErrorCode = "backup_in_progress"
	CodeBackupNotFound             ErrorCode = "backup_not_found"
	CodeBackupsDisabled            ErrorCode = "backups_disabled"
	CodeBackupsUnavailable         ErrorCode = "backups_unavailable"
	CodeCatalogManaged             ErrorCode = "catalog_managed"
	CodeCatalogRetired             ErrorCode = "catalog_retired"
	CodeCatalogUnavailable         ErrorCode = "catalog_unavailable"
	CodeConflict                   ErrorCode = "conflict"
	CodeDiskShrinkNotAllowed       ErrorCode = "disk_shrink_not_allowed"
	CodeEmailTaken                 ErrorCode = "email_taken"
	CodeEnrollmentExpired          ErrorCode = "enrollment_expired"
	CodeEnrollmentInvalid          ErrorCode = "enrollment_invalid"
	CodeEnrollmentState            ErrorCode = "enrollment_state"
	CodeEnrollmentUsed             ErrorCode = "enrollment_used"
	CodeFingerprintMismatch        ErrorCode = "fingerprint_mismatch"
	CodeFloatingIPsAttached        ErrorCode = "floating_ips_attached"
	CodeForbidden                  ErrorCode = "forbidden"
	CodeInternalError              ErrorCode = "internal_error"
	CodeInvalidCode                ErrorCode = "invalid_code"
	CodeInvalidCredentials         ErrorCode = "invalid_credentials"
	CodeInvalidInput               ErrorCode = "invalid_input"
	CodeInvalidReport              ErrorCode = "invalid_report"
	CodeInviteEmailMismatch        ErrorCode = "invite_email_mismatch"
	CodeIPInUse                    ErrorCode = "ip_in_use"
	CodeIPPoolExhausted            ErrorCode = "ip_pool_exhausted"
	CodeLastAdmin                  ErrorCode = "last_admin"
	CodeLastOwner                  ErrorCode = "last_owner"
	CodeLimitExceeded              ErrorCode = "limit_exceeded"
	CodeLocationInUse              ErrorCode = "location_in_use"
	CodeLocked                     ErrorCode = "locked"
	CodeLoginRequired              ErrorCode = "login_required"
	CodeNameTaken                  ErrorCode = "name_taken"
	CodeNoPrivateNetworksAvailable ErrorCode = "no_private_networks_available"
	CodeNotFound                   ErrorCode = "not_found"
	CodePasskeyExists              ErrorCode = "passkey_exists"
	CodePasskeyFailed              ErrorCode = "passkey_failed"
	CodePasskeyInvalid             ErrorCode = "passkey_invalid"
	CodePasskeyLimit               ErrorCode = "passkey_limit"
	CodePoolInUse                  ErrorCode = "pool_in_use"
	CodePoolOverlap                ErrorCode = "pool_overlap"
	CodeProjectNotEmpty            ErrorCode = "project_not_empty"
	CodePVEUnreachable             ErrorCode = "pve_unreachable"
	CodeQuotaExceeded              ErrorCode = "quota_exceeded"
	CodeRateLimitExceeded          ErrorCode = "rate_limit_exceeded"
	CodeRateLimited                ErrorCode = "rate_limited"
	CodeResourceInUse              ErrorCode = "resource_in_use"
	CodeResourceUnavailable        ErrorCode = "resource_unavailable"
	CodeSelfAdminAction            ErrorCode = "self_admin_action"
	CodeServerChanged              ErrorCode = "server_changed"
	CodeServerHasIP                ErrorCode = "server_has_ip"
	CodeServerMissing              ErrorCode = "server_missing"
	CodeServerNotReady             ErrorCode = "server_not_ready"
	CodeServerNotRunning           ErrorCode = "server_not_running"
	CodeServerNotStopped           ErrorCode = "server_not_stopped"
	CodeServiceUnavailable         ErrorCode = "service_unavailable"
	CodeSnapshotInUse              ErrorCode = "snapshot_in_use"
	CodeSnapshotNotReady           ErrorCode = "snapshot_not_ready"
	CodeSnapshotServerMismatch     ErrorCode = "snapshot_server_mismatch"
	CodeSnapshotTooLarge           ErrorCode = "snapshot_too_large"
	CodeSnapshotsUnavailable       ErrorCode = "snapshots_unavailable"
	CodeSoleOwner                  ErrorCode = "sole_owner"
	CodeTemplateNotFound           ErrorCode = "template_not_found"
	CodeTemplateNotShared          ErrorCode = "template_not_shared"
	CodeTOTPAlreadyEnabled         ErrorCode = "totp_already_enabled"
	CodeTOTPNotEnrolled            ErrorCode = "totp_not_enrolled"
	CodeTOTPRequired               ErrorCode = "totp_required"
	CodeUnauthorized               ErrorCode = "unauthorized"
	CodeUniquenessError            ErrorCode = "uniqueness_error"
	CodeVolumeAttached             ErrorCode = "volume_attached"
	CodeVolumeLimit                ErrorCode = "volume_limit"
	CodeVolumeNotAttached          ErrorCode = "volume_not_attached"
	CodeVolumeNotReady             ErrorCode = "volume_not_ready"
	CodeVolumeServerMismatch       ErrorCode = "volume_server_mismatch"
	CodeVolumeShrinkNotAllowed     ErrorCode = "volume_shrink_not_allowed"
	CodeVolumesUnavailable         ErrorCode = "volumes_unavailable"
	CodeUnreachable                ErrorCode = "unreachable"
	CodeTimedOut                   ErrorCode = "timed_out"
	CodeShutdownTimeout            ErrorCode = "shutdown_timeout"
	CodeServerGone                 ErrorCode = "server_gone"
	CodeReleasePending             ErrorCode = "release_pending"
	CodePowerOffRequired           ErrorCode = "power_off_required"
	CodeOrphaned                   ErrorCode = "orphaned"
	CodeFirewallUnenforced         ErrorCode = "firewall_unenforced"
	CodeFailed                     ErrorCode = "failed"
)

// Sentinels for errors.Is matching on an error code.
var (
	ErrAlreadyMember              = &Error{Code: CodeAlreadyMember}
	ErrAlreadySetup               = &Error{Code: CodeAlreadySetup}
	ErrBackupBusy                 = &Error{Code: CodeBackupBusy}
	ErrBackupInProgress           = &Error{Code: CodeBackupInProgress}
	ErrBackupNotFound             = &Error{Code: CodeBackupNotFound}
	ErrBackupsDisabled            = &Error{Code: CodeBackupsDisabled}
	ErrBackupsUnavailable         = &Error{Code: CodeBackupsUnavailable}
	ErrCatalogManaged             = &Error{Code: CodeCatalogManaged}
	ErrCatalogRetired             = &Error{Code: CodeCatalogRetired}
	ErrCatalogUnavailable         = &Error{Code: CodeCatalogUnavailable}
	ErrConflict                   = &Error{Code: CodeConflict}
	ErrDiskShrinkNotAllowed       = &Error{Code: CodeDiskShrinkNotAllowed}
	ErrEmailTaken                 = &Error{Code: CodeEmailTaken}
	ErrEnrollmentExpired          = &Error{Code: CodeEnrollmentExpired}
	ErrEnrollmentInvalid          = &Error{Code: CodeEnrollmentInvalid}
	ErrEnrollmentState            = &Error{Code: CodeEnrollmentState}
	ErrEnrollmentUsed             = &Error{Code: CodeEnrollmentUsed}
	ErrFingerprintMismatch        = &Error{Code: CodeFingerprintMismatch}
	ErrFloatingIPsAttached        = &Error{Code: CodeFloatingIPsAttached}
	ErrForbidden                  = &Error{Code: CodeForbidden}
	ErrInternalError              = &Error{Code: CodeInternalError}
	ErrInvalidCode                = &Error{Code: CodeInvalidCode}
	ErrInvalidCredentials         = &Error{Code: CodeInvalidCredentials}
	ErrInvalidInput               = &Error{Code: CodeInvalidInput}
	ErrInvalidReport              = &Error{Code: CodeInvalidReport}
	ErrInviteEmailMismatch        = &Error{Code: CodeInviteEmailMismatch}
	ErrIPInUse                    = &Error{Code: CodeIPInUse}
	ErrIPPoolExhausted            = &Error{Code: CodeIPPoolExhausted}
	ErrLastAdmin                  = &Error{Code: CodeLastAdmin}
	ErrLastOwner                  = &Error{Code: CodeLastOwner}
	ErrLimitExceeded              = &Error{Code: CodeLimitExceeded}
	ErrLocationInUse              = &Error{Code: CodeLocationInUse}
	ErrLocked                     = &Error{Code: CodeLocked}
	ErrLoginRequired              = &Error{Code: CodeLoginRequired}
	ErrNameTaken                  = &Error{Code: CodeNameTaken}
	ErrNoPrivateNetworksAvailable = &Error{Code: CodeNoPrivateNetworksAvailable}
	ErrNotFound                   = &Error{Code: CodeNotFound}
	ErrPasskeyExists              = &Error{Code: CodePasskeyExists}
	ErrPasskeyFailed              = &Error{Code: CodePasskeyFailed}
	ErrPasskeyInvalid             = &Error{Code: CodePasskeyInvalid}
	ErrPasskeyLimit               = &Error{Code: CodePasskeyLimit}
	ErrPoolInUse                  = &Error{Code: CodePoolInUse}
	ErrPoolOverlap                = &Error{Code: CodePoolOverlap}
	ErrProjectNotEmpty            = &Error{Code: CodeProjectNotEmpty}
	ErrPVEUnreachable             = &Error{Code: CodePVEUnreachable}
	ErrQuotaExceeded              = &Error{Code: CodeQuotaExceeded}
	ErrRateLimitExceeded          = &Error{Code: CodeRateLimitExceeded}
	ErrRateLimited                = &Error{Code: CodeRateLimited}
	ErrResourceInUse              = &Error{Code: CodeResourceInUse}
	ErrResourceUnavailable        = &Error{Code: CodeResourceUnavailable}
	ErrSelfAdminAction            = &Error{Code: CodeSelfAdminAction}
	ErrServerChanged              = &Error{Code: CodeServerChanged}
	ErrServerHasIP                = &Error{Code: CodeServerHasIP}
	ErrServerMissing              = &Error{Code: CodeServerMissing}
	ErrServerNotReady             = &Error{Code: CodeServerNotReady}
	ErrServerNotRunning           = &Error{Code: CodeServerNotRunning}
	ErrServerNotStopped           = &Error{Code: CodeServerNotStopped}
	ErrServiceUnavailable         = &Error{Code: CodeServiceUnavailable}
	ErrSnapshotInUse              = &Error{Code: CodeSnapshotInUse}
	ErrSnapshotNotReady           = &Error{Code: CodeSnapshotNotReady}
	ErrSnapshotServerMismatch     = &Error{Code: CodeSnapshotServerMismatch}
	ErrSnapshotTooLarge           = &Error{Code: CodeSnapshotTooLarge}
	ErrSnapshotsUnavailable       = &Error{Code: CodeSnapshotsUnavailable}
	ErrSoleOwner                  = &Error{Code: CodeSoleOwner}
	ErrTemplateNotFound           = &Error{Code: CodeTemplateNotFound}
	ErrTemplateNotShared          = &Error{Code: CodeTemplateNotShared}
	ErrTOTPAlreadyEnabled         = &Error{Code: CodeTOTPAlreadyEnabled}
	ErrTOTPNotEnrolled            = &Error{Code: CodeTOTPNotEnrolled}
	ErrTOTPRequired               = &Error{Code: CodeTOTPRequired}
	ErrUnauthorized               = &Error{Code: CodeUnauthorized}
	ErrUniquenessError            = &Error{Code: CodeUniquenessError}
	ErrVolumeAttached             = &Error{Code: CodeVolumeAttached}
	ErrVolumeLimit                = &Error{Code: CodeVolumeLimit}
	ErrVolumeNotAttached          = &Error{Code: CodeVolumeNotAttached}
	ErrVolumeNotReady             = &Error{Code: CodeVolumeNotReady}
	ErrVolumeServerMismatch       = &Error{Code: CodeVolumeServerMismatch}
	ErrVolumeShrinkNotAllowed     = &Error{Code: CodeVolumeShrinkNotAllowed}
	ErrVolumesUnavailable         = &Error{Code: CodeVolumesUnavailable}
	ErrUnreachable                = &Error{Code: CodeUnreachable}
	ErrTimedOut                   = &Error{Code: CodeTimedOut}
	ErrShutdownTimeout            = &Error{Code: CodeShutdownTimeout}
	ErrServerGone                 = &Error{Code: CodeServerGone}
	ErrReleasePending             = &Error{Code: CodeReleasePending}
	ErrPowerOffRequired           = &Error{Code: CodePowerOffRequired}
	ErrOrphaned                   = &Error{Code: CodeOrphaned}
	ErrFirewallUnenforced         = &Error{Code: CodeFirewallUnenforced}
	ErrFailed                     = &Error{Code: CodeFailed}
)

// Error is an error response from the API.
type Error struct {
	StatusCode int
	Code       ErrorCode
	Message    string
}

func (e *Error) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return fmt.Sprintf("%s (%s)", e.Message, e.Code)
}

// Is reports whether target is an *Error with the same non-empty code.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Code != "" && t.Code == e.Code
}

// codedError is implemented by errors that carry an API error code.
type codedError interface{ APICode() ErrorCode }

// APICode returns the error code.
func (e *Error) APICode() ErrorCode { return e.Code }

// IsCode reports whether err carries one of the given API error codes.
func IsCode(err error, codes ...ErrorCode) bool {
	var ce codedError
	if !errors.As(err, &ce) {
		return false
	}
	got := ce.APICode()
	for _, c := range codes {
		if got == c {
			return true
		}
	}
	return false
}
