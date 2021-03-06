package servicebus

import (
	"context"
	"testing"
	"time"

	"github.com/Azure/azure-amqp-common-go/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (suite *serviceBusSuite) TestMessageSession() {
	tests := map[string]func(context.Context, *testing.T, *MessageSession){
		"TestStateRoundTrip": testStateRoundTrip,
		"TestEmptyState":     testEmptyLock,
		"TestRenewLock":      testRenewLock,
	}

	ns := suite.getNewSasInstance()

	window := 30 * time.Second

	for name, testFunc := range tests {
		suite.T().Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
			defer cancel()

			queueName := suite.randEntityName()
			cleanup := makeQueue(
				ctx,
				t,
				ns,
				queueName,
				QueueEntityWithRequiredSessions(),
				QueueEntityWithDuplicateDetection(&window))
			defer cleanup()

			q, err := ns.NewQueue(queueName)
			defer func() {
				q.Close(context.Background())
			}()
			suite.NoError(err)

			var sessionID string
			if rawSession, err := uuid.NewV4(); err == nil {
				sessionID = rawSession.String()
			} else {
				t.Error(err)
				return
			}

			const want = "I rode my bicycle past your window last night"
			msg := NewMessageFromString(want)
			msg.GroupID = &sessionID

			suite.NoError(q.Send(ctx, msg))

			q.ReceiveOneSession(ctx, &sessionID, NewSessionHandler(
				HandlerFunc(func(ctx context.Context, msg *Message) DispositionAction {
					defer cancel()
					assert.Equal(t, string(msg.Data), want)
					return msg.Complete()
				}),
				func(ms *MessageSession) error {
					testFunc(ctx, t, ms)
					return nil
				},
				func() {}))
		})
	}
}

func testStateRoundTrip(ctx context.Context, t *testing.T, ms *MessageSession) {
	const want = "I roller-skated to your door at daylight"
	require.NoError(t, ms.SetState(ctx, []byte(want)))

	got, err := ms.State(ctx)
	require.NoError(t, err)

	if string(got) != want {
		t.Logf("\ngot:\n\t%qwant:\n\t%q", string(got), want)
		t.Fail()
	}
}

func testRenewLock(ctx context.Context, t *testing.T, ms *MessageSession) {
	original := ms.LockedUntil()
	require.NoError(t, ms.RenewLock(ctx))
	modified := ms.LockedUntil()

	if testing.Verbose() {
		t.Logf("\n\tnow:              \t%v\n\tupdated expiration:\t%v", time.Now().UTC(), modified)
	}

	if modified.Before(original) {
		t.Logf("\n\toriginal: %v\n\tmodified: %v\n\texpected a value greater than the original", original, modified)
		t.Fail()
	} else if modified == original {
		t.Logf("\n\toriginal: %v\n\tmodified: %v\n\tvalue didn't change", original, modified)
		t.Fail()
	} else if modified.After(time.Now().Add(3 * 24 * time.Hour)) {
		t.Logf("\n\toriginal: %v\n\tmodified: %v\n\tvalue is too far in the future.", original, modified)
		t.Fail()
	}
}

func testEmptyLock(ctx context.Context, t *testing.T, ms *MessageSession) {
	currentState, err := ms.State(ctx)
	require.NoError(t, err)
	assert.Nil(t, currentState)
}
