package hub

import "time"

const DefaultRoom string = "home"
const MaxReadBufferSize int = 1 << 10 // 1KB
const MaxWriteBufferSize int = 1 << 10
const MaxTimeLimitForPong time.Duration = 60
const MaxTimeLimitForPing time.Duration = 30
const MaxReadBufferSizeForFrames int = 1 << 20 // 1MB
