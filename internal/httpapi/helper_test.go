package httpapi

import "AgentScope/internal/auth"

const testSessionSecret = "0123456789abcdef0123456789abcdef"

// testSigner is a fixed secret signer used for all handler tests.
var testSigner = auth.NewSessionSigner([]byte(testSessionSecret))
