<!--  SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
SPDX-License-Identifier: Apache-2.0
-->

# audittools

`audittools` provides a standard interface for generating and sending CADF (Cloud Auditing Data Federation) audit events to a RabbitMQ message broker.

## Certification Requirements (PCI DSS, SOC 2, and more)

As a cloud provider subject to strict audits (including PCI DSS and more), we must ensure the **completeness** and **integrity** of audit logs while maintaining service **availability**.

### Standard Production Configuration

**You MUST configure a persistent backing store (SQL or File-Based with PVC).**

*   **Option 1 - SQL/Database Backing Store (Recommended)**:
    *   Configure `BackingStoreFactories` with `SQLBackingStoreFactoryWithDB(db)` using an existing PostgreSQL database
    *   **Advantages**: No volume management, leverages existing database infrastructure
    *   **Use Case**: Services that already have a database connection (most SAP services)

*   **Option 2 - File-Based with PVC**:
    *   Configure `BackingStoreFactories` with `NewFileBackingStore` and mount a PVC to the backing store directory
    *   **Use Case**: Services without database access but with volume support

*   **Requirement**: This ensures that audit events are preserved even in double-failure scenarios (RabbitMQ outage + Pod crash/reschedule).
*   **Compliance**: Satisfies requirements for guaranteed event delivery and audit trail completeness.

### Non-Compliant Configurations

The following configurations are available for development or specific edge cases but are **NOT** recommended for production services subject to audit:

1.  **File-Based with Ephemeral Storage (emptyDir)**:
    *   *Risk*: Data loss if the Pod is rescheduled during a RabbitMQ outage.
    *   *Status*: **Development / Testing Only**.

2.  **In-Memory Backing Store**:
    *   *Behavior*: Events are buffered in memory only. Data loss occurs if the Pod crashes during a RabbitMQ outage.
    *   *Use Case*: Services without persistent volumes that prefer limited buffering over service downtime.
    *   *Status*: **Development / Non-Compliant Environments Only**.

## Usage

### Basic Setup

To use `audittools`, you typically initialize an `Auditor` with your RabbitMQ connection details and backing store factories.

```go
import (
    "context"
    "github.com/sapcc/go-bits/audittools"
)

func main() {
    // ...
    auditor, err := audittools.NewAuditor(context.Background(), audittools.AuditorOpts{
        Observer: audittools.Observer{
            TypeURI: "service/myservice",
            Name:    "myservice",
            ID:      "instance-uuid",
        },
        EnvPrefix: "MYSERVICE_AUDIT", // Configures env vars like MYSERVICE_AUDIT_RABBITMQ_URL
        BackingStoreFactories: map[string]audittools.BackingStoreFactory{
            "file":   audittools.NewFileBackingStore,
            "memory": audittools.NewInMemoryBackingStore,
        },
    })
    if err != nil {
        log.Fatal(err)
    }
    // ...
}
```

### Sending Events

```go
event := cadf.Event{
    // ... fill in event details ...
}
auditor.Record(event)
```

## Event Buffering with Backing Stores

`audittools` includes pluggable backing stores to ensure audit events are not lost if the RabbitMQ broker becomes unavailable. Events are temporarily buffered and replayed once the connection is restored.

### Backing Store Types

The backing store is configured via JSON and supports multiple implementations:

1.  **SQL/Database Backing Store** (`type: "sql"`):
    *   Persists events to a PostgreSQL database table
    *   Survives pod restarts and database restarts
    *   **Recommended for services that already have a database connection**
    *   No filesystem volume management required
    *   Leverages existing database infrastructure

2.  **File-Based Backing Store** (`type: "file"`):
    *   Persists events to local filesystem files
    *   Survives pod restarts when using persistent volumes
    *   Recommended for production services without existing database connections

3.  **In-Memory Backing Store** (`type: "memory"`):
    *   Buffers events in process memory
    *   Does not survive pod restarts
    *   Suitable for development or services without persistent volumes

### Configuration

#### Programmatic Configuration

**SQL/Database Backing Store** (Recommended):
```go
// First, open your database connection (or reuse existing connection)
db, err := sql.Open("postgres", dsn)
// ... handle error

// Create auditor with SQL backing store factory
auditor, err := audittools.NewAuditor(context.Background(), audittools.AuditorOpts{
    Observer: audittools.Observer{
        TypeURI: "service/myservice",
        Name:    "myservice",
        ID:      "instance-uuid",
    },
    EnvPrefix: "MYSERVICE_AUDIT",
    BackingStoreFactories: map[string]audittools.BackingStoreFactory{
        "sql": audittools.SQLBackingStoreFactoryWithDB(db),
    },
})
// The backing store type and params are configured via the environment variable:
// MYSERVICE_AUDIT_BACKING_STORE='{"type":"sql","params":{"table_name":"audit_events","max_events":10000}}'
```

**File-Based Backing Store**:
```go
auditor, err := audittools.NewAuditor(context.Background(), audittools.AuditorOpts{
    Observer: audittools.Observer{
        TypeURI: "service/myservice",
        Name:    "myservice",
        ID:      "instance-uuid",
    },
    EnvPrefix: "MYSERVICE_AUDIT",
    BackingStoreFactories: map[string]audittools.BackingStoreFactory{
        "file": audittools.NewFileBackingStore,
    },
})
// Set environment variable:
// MYSERVICE_AUDIT_BACKING_STORE='{"type":"file","params":{"directory":"/var/lib/myservice/audit-buffer","max_total_size":1073741824}}'
```

**In-Memory Backing Store**:
```go
auditor, err := audittools.NewAuditor(context.Background(), audittools.AuditorOpts{
    Observer: audittools.Observer{
        TypeURI: "service/myservice",
        Name:    "myservice",
        ID:      "instance-uuid",
    },
    EnvPrefix: "MYSERVICE_AUDIT",
    BackingStoreFactories: map[string]audittools.BackingStoreFactory{
        "memory": audittools.NewInMemoryBackingStore,
    },
})
// Set environment variable:
// MYSERVICE_AUDIT_BACKING_STORE='{"type":"memory","params":{"max_events":1000}}'
```

**Direct BackingStore Instance** (Advanced):
```go
// You can also provide a backing store instance directly:
backingStore := &audittools.InMemoryBackingStore{}
backingStore.Init(prometheus.DefaultRegisterer)

auditor, err := audittools.NewAuditor(context.Background(), audittools.AuditorOpts{
    Observer: audittools.Observer{
        TypeURI: "service/myservice",
        Name:    "myservice",
        ID:      "instance-uuid",
    },
    EnvPrefix:    "MYSERVICE_AUDIT",
    BackingStore: backingStore, // Use this instance directly
})
```

#### Environment Variable Configuration

*   `MYSERVICE_AUDIT_BACKING_STORE`: JSON configuration string

Examples:

*   SQL/Database: `{"type":"sql","params":{"table_name":"audit_events","max_events":10000}}`
*   File-based: `{"type":"file","params":{"directory":"/var/cache/audit","max_file_size":10485760,"max_total_size":1073741824}}`
*   In-memory: `{"type":"memory","params":{"max_events":1000}}`

If no `${PREFIX}_BACKING_STORE` environment variable is set, a default in-memory backing store with 1000 events capacity is used.

#### SQL/Database Parameters

**Important**: SQL backing stores require applications to provide their own database connection using `SQLBackingStoreFactoryWithDB()` in the `BackingStoreFactories` map. This prevents duplicate connection pools and allows applications to reuse existing database connections.

*   `table_name` (optional): Table name for storing events (default: `audit_events`)
*   `batch_size` (optional): Number of events to read per batch (default: 100)
*   `max_events` (optional): Maximum total events to buffer (default: 10000)
*   `skip_migration` (optional): Skip automatic table creation (default: false)

**Database Setup**: The backing store will automatically create the required table unless `skip_migration` is true. For manual migration, see [`backing_store_sql_migration.sql`](backing_store_sql_migration.sql).

Each auditor has its own factory map, allowing different auditors in the same application to use different database connections or backing store implementations without global state pollution.

#### File-Based Parameters

*   `directory` (required): Directory to store buffered event files
*   `max_file_size` (optional): Maximum size per file in bytes (default: 10 MB)
*   `max_total_size` (optional): Maximum total size of all files in bytes (no limit if not set)

#### In-Memory Parameters

*   `max_events` (optional): Maximum number of events to buffer (default: 1000)

### Kubernetes Deployment

If running in Kubernetes, you have several options for configuring the backing store:

1.  **SQL/Database Backing Store (Recommended for most SAP services)**:
    *   Connect to an existing PostgreSQL database (e.g., service's main database).
    *   **Pros**: Data survives Pod deletion and rescheduling. No volume management. Leverages existing database infrastructure.
    *   **Cons**: Requires database access and table creation privileges.
    *   **Use Case**: **Recommended** for services that already have a database connection. Ideal for audit compliance without volume management overhead.
    *   **Configuration**: Applications must provide `SQLBackingStoreFactoryWithDB(db)` in the `BackingStoreFactories` map with their existing database connection, then set environment variable `MYSERVICE_AUDIT_BACKING_STORE='{"type":"sql","params":{"table_name":"audit_events","max_events":10000}}'`

2.  **File-Based with Persistent Storage (PVC)**:
    *   Mount a Persistent Volume Claim (PVC) and configure a file-based backing store pointing to that mount.
    *   **Pros**: Data survives Pod deletion, rescheduling, and rolling updates. No database required.
    *   **Cons**: Adds complexity (volume management, access modes, storage provisioning).
    *   **Use Case**: Services without database access but with volume support.
    *   **Configuration**: `{"type":"file","params":{"directory":"/mnt/pvc/audit-buffer","max_total_size":1073741824}}`

3.  **File-Based with Ephemeral Storage (emptyDir)**:
    *   Mount an `emptyDir` volume and configure a file-based backing store.
    *   **Pros**: Simple, fast, no persistent volume management. Data survives container restarts within the same Pod.
    *   **Cons**: Data is lost if the Pod is deleted or rescheduled.
    *   **Use Case**: Suitable for non-critical environments or where occasional data loss during complex failure scenarios is acceptable.
    *   **Configuration**: `{"type":"file","params":{"directory":"/tmp/audit-buffer"}}`

4.  **In-Memory Backing Store**:
    *   No volume mount or database required.
    *   **Pros**: Simplest configuration, no storage management overhead.
    *   **Cons**: Data is lost on any Pod restart or crash. Limited buffer capacity.
    *   **Use Case**: Development environments or services that prefer limited buffering over any storage complexity.
    *   **Configuration**: `{"type":"memory","params":{"max_events":1000}}` (or omit config entirely for default)

### Behavior

The system transitions through the following states to ensure zero data loss:

1.  **Normal Operation**: Events are sent directly to RabbitMQ.
2.  **RabbitMQ Outage**: Events are written to the backing store (file or memory). The application continues without blocking.
3.  **Backing Store Full**: If the backing store reaches its capacity limit, writes fail and `auditor.Record()` **blocks**. This pauses the application to prevent data loss.
    *   File-based: Controlled by `max_total_size` parameter
    *   In-memory: Controlled by `max_events` parameter
4.  **Recovery**: A background routine continuously drains the backing store to RabbitMQ once it becomes available. New events are buffered during draining to prevent blocking.
    *   **Note**: Strict chronological ordering is not guaranteed during recovery. New events are sent immediately if the connection is up, while old events from the backing store are drained asynchronously.

**Additional Details**:

*   **Security (File-Based Only)**: The directory is created with `0700` permissions, and files with `0600`, ensuring only the service user can access the sensitive audit data.
*   **Capacity**:
    *   File-based: The `max_total_size` limit is approximate and may be exceeded by up to one event's size (typically a few KB) due to the check-then-write sequence. Set the limit with appropriate headroom for your filesystem.
    *   In-memory: The `max_events` limit is strictly enforced.
*   **Corrupted Event Handling (File-Based Only)**:
    *   Corrupted events encountered during reads are written to dead-letter files (`audit-events-deadletter-*.jsonl`)
    *   Dead-letter files contain metadata (timestamp, source file) and the raw corrupted data for investigation
    *   The `corrupted_event` metric is incremented for monitoring
    *   Source files are deleted after processing, even if all events were corrupted (after moving to dead-letter)
    *   Dead-letter files should be monitored and investigated to identify data corruption issues

### Delivery Guarantees

This library aims to provide reliability guarantees similar to OpenStack's `oslo.messaging` (used by Keystone Middleware).

1.  **At-Least-Once Delivery**: The primary guarantee is "at-least-once" delivery. Events are persisted to disk if the broker is unavailable and retried until successful.
    *   *Note*: If a batch of events partially fails to send, the **entire batch** is retried. This ensures no data is lost but may result in duplicate events being sent to the broker. Consumers should implement idempotency using the event `ID` to handle these duplicates, similar to how `oslo.messaging` consumers are expected to behave.
2.  **Ordering**: Strict chronological ordering is **not guaranteed** during recovery. New events are sent immediately if the connection is up, while old events from the backing store are drained asynchronously. This aligns with the behavior of many distributed message queues where availability is prioritized over strict ordering during partitions.

### Metrics

The backing store exports the following Prometheus metrics:

**Common Metrics (All Backing Store Types)**:

*   `audittools_backing_store_writes_total`: Total number of audit events written to the backing store.
*   `audittools_backing_store_reads_total`: Total number of audit events read from the backing store.
*   `audittools_backing_store_size_bytes`: Current size of the backing store.
    *   File-based: Total size in bytes
    *   In-memory: Number of events

**File-Based Backing Store Metrics**:

*   `audittools_backing_store_files_count`: Current number of files in the backing store.
*   `audittools_backing_store_errors_total`: Total number of errors, labeled by operation:
    *   `write_stat`: Failed to stat file during rotation check
    *   `write_full`: Backing store is full (exceeds `max_total_size`)
    *   `write_open`: Failed to open backing store file for writing
    *   `write_marshal`: Failed to marshal event to JSON
    *   `write_io`: Failed to write event to disk
    *   `write_sync`: Failed to sync (flush) event to disk
    *   `write_close`: Failed to close backing store file
    *   `read_open`: Failed to open backing store file for reading
    *   `read_scan`: Failed to scan backing store file
    *   `corrupted_event`: Encountered corrupted event during read (written to dead-letter)
    *   `deadletter_write`: Successfully wrote corrupted event to dead-letter file
    *   `deadletter_write_failed`: Failed to write corrupted event to dead-letter file
    *   `commit_remove`: Failed to remove file after successful processing
