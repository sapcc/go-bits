/*******************************************************************************
*
* Copyright 2019 SAP SE
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You should have received a copy of the License along with this
* program. If not, you may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
*******************************************************************************/

package audittools_test

import (
	"context"
	"net"
	"net/url"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-api-declarations/cadf"
	"github.com/sapcc/go-bits/audittools"
)

var eventPublishSuccessCounter = prometheus.NewCounter(
	prometheus.CounterOpts{
		Name: "yourApplication_successful_auditevent_publish",
		Help: "Counter for successful audit event publish to RabbitMQ server.",
	},
)
var eventPublishFailedCounter = prometheus.NewCounter(
	prometheus.CounterOpts{
		Name: "yourApplication_failed_auditevent_publish",
		Help: "Counter for failed audit event publish to RabbitMQ server.",
	},
)

func InitAuditTrail(ctx context.Context) chan<- cadf.Event {
	onSuccessFunc := func() {
		eventPublishSuccessCounter.Inc()
	}
	onFailedFunc := func() {
		eventPublishFailedCounter.Inc()
	}

	rabbitmqQueueName := "down-the-rabbit-hole"
	rabbitmqURI := url.URL{
		Scheme: "amqp",
		Host:   net.JoinHostPort("localhost", "5672"),
		User:   url.UserPassword("guest", "guest"),
		Path:   "/",
	}

	s := make(chan cadf.Event, 20)
	go audittools.AuditTrail{
		EventSink:           s,
		OnSuccessfulPublish: onSuccessFunc,
		OnFailedPublish:     onFailedFunc,
	}.Commit(ctx, rabbitmqURI, rabbitmqQueueName)
	return s
}

func ExampleAuditTrail() {
	// in setup phase
	eventSink := InitAuditTrail(context.TODO())

	// at event generation time (e.g. in HTTP request handler)
	var params audittools.EventParameters // TODO: fill this according to the event being processed
	eventSink <- audittools.NewEvent(params)
}
