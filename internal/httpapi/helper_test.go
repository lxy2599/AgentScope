package httpapi

import "AgentScope/internal/auth"

// testSigner is a fixed secret signer used for all handler tests.
var testSigner = auth.NewSessionSigner([]byte("test-secret-must-be-16-bytes"))
