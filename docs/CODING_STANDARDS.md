# Coding Standards

## File header

Every file starts with:

```go
// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0
```

## Error handling pattern

```go
// Good: wrap with context
if err := session.Establish(ctx); err != nil {
    return fmt.Errorf("establishing PASE session: %w", err)
}

// Good: sentinel errors for expected conditions
var ErrDeviceNotFound = errors.New("device not found")
var ErrSessionExpired = errors.New("session expired")
```

## Logging

Use `log/slog` (Go 1.21+ structured logging):

```go
slog.Debug("sending read request",
    "node", nodeID,
    "endpoint", endpoint,
    "cluster", clusterName,
    "attribute", attrName,
)
```

## Test pattern

Use table-driven tests with `t.Run` subtests:

```go
func TestTLVEncodeUint(t *testing.T) {
    tests := []struct {
        name     string
        tag      tlv.Tag
        value    uint64
        expected []byte
    }{
        {"uint8 zero", tlv.ContextTag(0), 0, []byte{0x24, 0x00, 0x00}},
        {"uint8 max", tlv.ContextTag(1), 255, []byte{0x24, 0x01, 0xFF}},
        {"uint16", tlv.ContextTag(2), 256, []byte{0x25, 0x02, 0x00, 0x01}},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            var buf bytes.Buffer
            w := tlv.NewWriter(&buf)
            err := w.PutUint(tt.tag, tt.value)
            require.NoError(t, err)
            assert.Equal(t, tt.expected, buf.Bytes())
        })
    }
}
```
