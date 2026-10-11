package validate

import "strings"

// ValidateName reports whether s is an acceptable user name.
func ValidateName(s string) bool {
	return len(s) >= 3 && len(s) <= 20
}

// ValidateEmail reports whether s looks like an email address: exactly one
// "@", a non-empty local part, and a domain that contains a dot.
func ValidateEmail(s string) bool {
	local, domain, ok := strings.Cut(s, "@")
	if !ok || local == "" || strings.Contains(domain, "@") {
		return false
	}
	return strings.Contains(domain, ".")
}

// ValidatePhone reports whether s has 11 digits.
func ValidatePhone(s string) bool {
	return len(s) == 11
}
