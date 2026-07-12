# Large Artifact Transfer

## Goal

Keep large screenshots, recordings, logs, and build outputs out of Control Plane task events while preserving resumable, verifiable transfer.

## Contract

Desktop actions that produce binary evidence may write a temporary target-local artifact and return only a compact reference: relative path, byte size, SHA-256, and content type. The Agent retrieves the reference through the file adapter in bounded chunks. Each read declares an offset and chunk size, returns the next offset and completion flag, and includes the final digest when complete. The Agent retries a failed chunk from the last verified offset and rejects a mismatched digest.

The inline result path remains available only for small results under the event budget. Large results must never be silently truncated or embedded in an event. Temporary artifacts are scoped to the attended session and cleaned after transfer or cancellation.

## Verification

Unit tests cover chunk boundaries, offsets, completion, and SHA-256 verification. Integration tests prove an oversized result is represented by metadata rather than inline content. Windows acceptance runs screenshot and recording, downloads their chunks, verifies the PNG bytes, and then proves keyboard and mouse tasks still execute on the same session.
