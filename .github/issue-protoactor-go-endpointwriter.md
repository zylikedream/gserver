# SenderMiddleware on RootContext crashes EndpointWriter with envelope-wrapped *remoteDeliver

**Description**

Registering a `SenderMiddleware` on `RootContext` (via `actor.WithSenderMiddleware`) causes `EndpointWriter` to crash with a type assertion panic, because internal `*remoteDeliver` messages become wrapped in `*MessageEnvelope` before reaching the custom mailbox.

**Root Cause**

The crash chain:

1. `EndpointManager` sends `*remoteDeliver` to `EndpointWriter` via `RootContext.Send(endpoint.writer, rd)`.
2. When `senderMiddleware != nil`, `RootContext.sendUserMessage` wraps the message in `*MessageEnvelope{Message: rd}` and passes it through the middleware chain.
3. `EndpointWriter` uses a **custom mailbox** (`endpointWriterMailbox`). Its `run()` method calls `PopMany(batchSize)` to batch messages:

   ```go
   // endpoint_writer_mailbox.go:107
   if msg, ok = m.userMailbox.PopMany(int64(m.batchSize)); ok {
       m.invoker.InvokeUserMessage(msg)  // msg is []interface{} batch
   }
   ```

   `PopMany` returns `[]interface{}`. With middleware, each element is `*MessageEnvelope` instead of raw `*remoteDeliver`.

4. The actor's `Receive` gets `ctx.Message()` which returns the `[]interface{}` as-is (`UnwrapEnvelopeMessage` only checks the top-level message, not elements inside a slice).
5. `EndpointWriter.sendEnvelopes` iterates over the batch with a hard type assertion:

   ```go
   // endpoint_writer.go
   for _, tmp := range batch {
       rd := tmp.(*remoteDeliver)  // panic: *MessageEnvelope is not *remoteDeliver
   }
   ```

**Why `ctx.Message()` auto-unwrap can't help**

`ctx.Message()` calls `UnwrapEnvelopeMessage(ctx.messageOrEnvelope)`, which only checks if the **top-level** message is `*MessageEnvelope`. When the message is `[]interface{}{envelope1, envelope2, ...}`, it returns the slice as-is. Elements inside the slice are never recursively unwrapped.

**Impact**

- Any `SenderMiddleware` on `RootContext` makes the actor system unusable for remote communication.

**Suggested Fixes**

1. **Make `UnwrapEnvelopeMessage` recursively unwrap elements inside `[]interface{}`** — though this changes framework-wide semantics.

**Code References**

- `remote/endpoint_writer_mailbox.go:107` — `PopMany` + `InvokeUserMessage`
- `remote/endpoint_writer.go` — `sendEnvelopes` with `tmp.(*remoteDeliver)` type assertion
- `actor/context.go` — `Message()` → `UnwrapEnvelopeMessage()`
- `actor/root_context.go` — `sendUserMessage` with middleware wrapping
- `actor/message_envelope.go` — `UnwrapEnvelopeMessage` one-level check
