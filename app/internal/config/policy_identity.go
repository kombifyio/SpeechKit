package config

import (
	"github.com/kombifyio/SpeechKit/app/internal/productid"
)

// PolicyIdentity names the product whose machine policy is read. It is the
// policy view of the registered product identity (app/internal/productid);
// nothing in the config package assumes a vendor or product name. From it
// derive:
//
//	HKLM\SOFTWARE\Policies\<Vendor>\<Product>   (GPO)
//	HKLM\SOFTWARE\<Vendor>\<Product>            (machine-wide admin values)
//	HKCU\Software\<Vendor>\<Product>            (per user, restrict-only)
//	/Library/Managed Preferences/<BundleID>.plist (macOS, MDM)
//
// A process that registers no identity cannot Load a configuration at all
// (productid.ErrNoIdentity), so a missing identity is never read as "no
// policy".
type PolicyIdentity struct {
	Vendor   string
	Product  string
	BundleID string
}

// PolicyOriginNoIdentity is the Origin of a policy read in a process that
// registered no product identity.
const PolicyOriginNoIdentity = "no-identity"

// ErrNoIdentity is returned by Load in a process whose composition root
// registered no product identity.
var ErrNoIdentity = productid.ErrNoIdentity

// RegisteredPolicyIdentity returns the policy identity of the registered
// product, ok=false when none was registered.
func RegisteredPolicyIdentity() (PolicyIdentity, bool) {
	p, err := productid.Current()
	if err != nil {
		return PolicyIdentity{}, false
	}
	return PolicyIdentity{Vendor: p.Vendor, Product: p.Product, BundleID: p.BundleID}, true
}

// noIdentityPolicy is the fail-closed result for a process without an
// identity: no source is read.
func noIdentityPolicy() PolicyValues {
	return PolicyValues{Origin: PolicyOriginNoIdentity}
}
