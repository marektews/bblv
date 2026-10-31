package config

import (
	"slices"

	"golang.org/x/sys/windows"
)

// ProtectDir lets only the system, the administrators and the owner of the
// data directory use it and everything in it: config.json holds the access
// codes, the password hash and the session secret, and printer-ca.pem
// decides which printers are trusted. The directory stops inheriting the
// permissions of its parent, which let every user read files, and on
// drives other than C: every signed-in user change them.
func ProtectDir(dir string) error {
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return err
	}
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return err
	}
	var granted []*windows.SID
	var entries []windows.EXPLICIT_ACCESS
	for _, sid := range []*windows.SID{system, admins, owner} {
		if slices.ContainsFunc(granted, sid.Equals) {
			continue // the owner is the system or the administrators
		}
		granted = append(granted, sid)
		entries = append(entries, windows.EXPLICIT_ACCESS{
			AccessPermissions: windows.GENERIC_ALL,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_UNKNOWN,
				TrusteeValue: windows.TrusteeValueFromSID(sid),
			},
		})
	}
	acl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		return err
	}
	// Existing files and directories inherit the new permissions.
	return windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}
