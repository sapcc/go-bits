// SPDX-FileCopyrightText: 2019 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audittools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/sapcc/go-api-declarations/cadf"
)

// dataplaneAuditQueueName is the well-known queue name used by the dataplane audit pipeline.
// This queue must be declared as durable so it survives broker restarts.
const dataplaneAuditQueueName = "dataplane.audit"

// rabbitConnection represents a unique connection to some RabbitMQ server with
// an open Channel and a declared Queue.
type rabbitConnection struct {
	Inner     *amqp.Connection
	Channel   *amqp.Channel
	QueueName string

	LastConnectedAt time.Time
}

// newRabbitConnection returns a new rabbitConnection using the specified amqp URI
// and queue name.
func newRabbitConnection(uri url.URL, queueName string) (rc *rabbitConnection, err error) {
	// establish a connection with the RabbitMQ server
	// (with a shorter timeout than the default of 30 seconds, since Record() blocks while we wait here)
	conn, err := amqp.DialConfig(uri.String(), amqp.Config{Dial: amqp.DefaultDial(5 * time.Second)})
	if err != nil {
		return nil, fmt.Errorf("audittools: rabbitmq: failed to establish a connection with the server: %w", err)
	}
	defer func() {
		if err != nil {
			conn.CloseDeadline(time.Now().Add(time.Second)) //nolint:errcheck // we are already returning the more relevant error
		}
	}()

	// open a unique, concurrent server channel to process the bulk of AMQP messages
	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("audittools: rabbitmq: failed to open a channel: %w", err)
	}

	// with publisher confirms, PublishEvent can wait until the server has taken responsibility for the event
	err = ch.Confirm(false)
	if err != nil {
		return nil, fmt.Errorf("audittools: rabbitmq: failed to enable publisher confirms: %w", err)
	}

	// declare a queue to hold and deliver messages to consumers
	_, err = ch.QueueDeclare(
		queueName,                            // name of the queue
		queueName == dataplaneAuditQueueName, // durable: survive broker restart for the dataplane audit queue
		false,                                // autodelete when unused
		false,                                // exclusive: queue only accessible by connection that declares and deleted when the connection closes
		false,                                // noWait: the queue will assume to be declared on the server
		nil,                                  // arguments for advanced config
	)
	if err != nil {
		return nil, fmt.Errorf("audittools: rabbitmq: failed to declare a queue: %w", err)
	}

	return &rabbitConnection{
		Inner:           conn,
		Channel:         ch,
		QueueName:       queueName,
		LastConnectedAt: time.Now(),
	}, nil
}

// Disconnect is a helper function for closing a rabbitConnection.
// Closing the connection also closes the channel.
// We do not wait for the server for more than a second, since it may not be responding.
func (c *rabbitConnection) Disconnect() {
	c.Inner.CloseDeadline(time.Now().Add(time.Second)) //nolint:errcheck // the connection is not used anymore either way
}

// IsNilOrClosed is like (*amqp.Connection).IsClosed() but it also returns true
// if rabbitConnection or the underlying amqp.Connection are nil, or if the channel is closed.
func (c *rabbitConnection) IsNilOrClosed() bool {
	return c == nil || c.Inner == nil || c.Inner.IsClosed() || c.Channel.IsClosed()
}

// PublishEvent publishes a cadf.Event to a specific RabbitMQ Connection.
// A nil pointer for event parameter will return an error.
func (c *rabbitConnection) PublishEvent(ctx context.Context, event *cadf.Event) error {
	if c.IsNilOrClosed() {
		return amqp.ErrClosed
	}

	if event == nil {
		return errors.New("audittools: could not publish event: got a nil pointer for 'event' parameter")
	}

	b, err := json.Marshal(event)
	if err != nil {
		return err
	}

	confirmation, err := c.Channel.PublishWithDeferredConfirmWithContext(
		ctx,
		"",          // exchange: publish to default
		c.QueueName, // routing key: same as queue name
		false,       // mandatory: don't publish if no queue is bound that matches the routing key
		false,       // immediate: don't publish if no consumer on the matched queue is ready to accept the delivery
		amqp.Publishing{
			ContentType: "text/plain",
			Body:        b,
		},
	)
	if err != nil {
		return err
	}

	// Without waiting for the confirm, events would be lost when the server stops responding,
	// since publishing only puts them in the socket buffer.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	acked, err := confirmation.WaitContext(ctx)
	if err != nil {
		// the server is not responding, so reconnect for the next event
		c.Disconnect()
		return fmt.Errorf("no publisher confirm from RabbitMQ: %w", err)
	}
	if !acked {
		return errors.New("RabbitMQ did not accept the event")
	}
	return nil
}
