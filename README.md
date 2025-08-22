# 🏢 Leave Management System (LMS) Backend

A comprehensive REST API backend for managing employee leave requests, built with Go, Gin framework, and PostgreSQL.

## 📋 Table of Contents

- [Features](#-features)
- [Architecture](#-architecture)
- [Tech Stack](#-tech-stack)
- [Database Schema](#-database-schema)
- [API Endpoints](#-api-endpoints)
- [Installation & Setup](#-installation--setup)
- [Environment Variables](#-environment-variables)
- [Usage Examples](#-usage-examples)
- [Validation Rules](#-validation-rules)
- [Error Handling](#-error-handling)
- [Testing](#-testing)








## ✨ Features

- **Employee Management**: Complete CRUD operations for employees
- **Leave Types Management**: Configurable leave types with carry-forward support
- **Leave Request Processing**: Apply, approve, reject, and cancel leave requests
- **Leave Balance Tracking**: Automatic balance allocation and deduction
- **Audit Logging**: Comprehensive audit trail for all operations
- **Business Logic Validation**: Date validation, overlap detection, balance checks
- **Database Triggers**: Automatic audit logging and balance allocation

## 🏗️ Architecture

```
Backend/
├── main.go                 # Application entry point
├── go.mod                  # Go module dependencies
├── internal/
│   ├── config/
│   │   └── config.go       # Configuration management
│   ├── db/
│   │   └── db.go          # Database connection pool
│   ├── handlers/
│   │   ├── employee_handler.go    # Employee CRUD operations
│   │   ├── leave_request.go       # Leave request processing
│   │   ├── leave_type.go          # Leave type management
│   │   └── audit_handler.go       # Audit logs retrieval
│   ├── models/
│   │   └── employee.go     # Data models
│   └── router/
│       └── router.go       # Route definitions
└── Database/
    └── db.sql             # Database schema and functions
```

## 🛠️ Tech Stack

- **Language**: Go 1.23+
- **Framework**: Gin (HTTP web framework)
- **Database**: PostgreSQL with pgx driver
- **Connection Pool**: pgxpool for efficient database connections
- **Validation**: Gin binding validation
- **Environment**: godotenv for configuration

## 🗄️ Database Schema

### Core Tables

#### 1. **departments**
- `id` (UUID, Primary Key)
- `name` (VARCHAR(100), Unique)
- `description` (TEXT)
- `manager_id` (UUID, Foreign Key)
- `created_at`, `updated_at` (Timestamps)

#### 2. **leave_types**
- `id` (UUID, Primary Key)
- `name` (VARCHAR(50), Unique)
- `description` (TEXT)
- `max_days_per_year` (INTEGER)
- `carry_forward_allowed` (BOOLEAN)
- `max_carry_forward_days` (INTEGER)
- `is_active` (BOOLEAN)
- `created_at`, `updated_at` (Timestamps)

#### 3. **employees**
- `id` (UUID, Primary Key)
- `employee_id` (VARCHAR(20), Unique)
- `email` (VARCHAR(255), Unique)
- `name` (VARCHAR(255))
- `department_id` (UUID, Foreign Key)
- `role` (ENUM: employee, hr, manager, admin)
- `joining_date` (DATE)
- `manager_id` (UUID, Foreign Key)
- `is_active` (BOOLEAN)
- `phone` (VARCHAR(15))
- `address` (TEXT)
- `created_at`, `updated_at` (Timestamps)

#### 4. **employee_leave_balances**
- `id` (UUID, Primary Key)
- `employee_id` (UUID, Foreign Key)
- `leave_type_id` (UUID, Foreign Key)
- `year` (INTEGER)
- `allocated_days` (INTEGER)
- `used_days` (INTEGER)
- `carried_forward_days` (INTEGER)
- `available_days` (GENERATED: allocated + carried_forward - used)
- `created_at`, `updated_at` (Timestamps)

#### 5. **leave_requests**
- `id` (UUID, Primary Key)
- `employee_id` (UUID, Foreign Key)
- `leave_type_id` (UUID, Foreign Key)
- `start_date` (DATE)
- `end_date` (DATE)
- `total_days` (INTEGER)
- `reason` (TEXT)
- `status` (ENUM: pending, approved, rejected, cancelled)
- `applied_at` (Timestamp)
- `approved_by` (UUID, Foreign Key)
- `approved_at` (Timestamp)
- `rejection_reason` (TEXT)
- `comments` (TEXT)
- `created_at`, `updated_at` (Timestamps)

#### 6. **audit_logs**
- `id` (UUID, Primary Key)
- `table_name` (VARCHAR(50))
- `record_id` (UUID)
- `action` (VARCHAR(20))
- `old_values` (JSONB)
- `new_values` (JSONB)
- `changed_by` (UUID, Foreign Key)
- `changed_at` (Timestamp)

### Database Functions

#### 1. **calculate_working_days(start_date, end_date)**
- Calculates working days (Monday-Friday) between two dates
- Returns INTEGER

#### 2. **check_leave_overlap(employee_id, start_date, end_date, exclude_request_id)**
- Checks for overlapping leave requests
- Returns BOOLEAN

### Triggers

- **Audit Triggers**: Automatic logging of INSERT, UPDATE, DELETE operations
- **Balance Allocation**: Automatic leave balance creation for new employees
- **Updated At**: Automatic timestamp updates

## 🔌 API Endpoints

### Health Check
```
GET /health
```
**Response**: `{"status": "ok"}`

### Employee Management

#### Create Employee
```
POST /employees
Content-Type: application/json

{
  "name": "John Doe",
  "email": "john.doe@company.com",
  "department_id": "uuid",
  "joining_date": "2024-01-15",
  "employee_id": "EMP-2024-001"  // Optional
}
```

#### List Employees
```
GET /employees?department_id=uuid&role=employee&active=true
```

#### Get Employee
```
GET /employees/{id}
```

#### Update Employee
```
PUT /employees/{id}
Content-Type: application/json

{
  "email": "new.email@company.com",
  "phone": "+1234567890",
  "department_id": "uuid",
  "role": "manager"
}
```

#### Deactivate Employee
```
DELETE /employees/{id}
```

#### Get Leave Balances
```
GET /employees/{id}/leave-balances
```

#### Update Leave Balances
```
PUT /employees/{id}/leave-balances
Content-Type: application/json

{
  "leave_type_id": "uuid",
  "allocated_days": 25,
  "used_days": 5,
  "carried_forward_days": 3,
  "year": 2024
}
```

### Leave Types Management

#### List Leave Types
```
GET /leave-types
```

#### Create Leave Type
```
POST /leave-types
Content-Type: application/json

{
  "name": "Annual Leave",
  "description": "Yearly vacation days",
  "max_days_per_year": 21,
  "carry_forward_allowed": true,
  "max_carry_forward_days": 5,
  "is_active": true
}
```

#### Update Leave Type
```
PUT /leave-types/{id}
Content-Type: application/json

{
  "name": "Updated Leave Type",
  "max_days_per_year": 25
}
```

#### Delete Leave Type
```
DELETE /leave-types/{id}
```

### Leave Requests Management

#### Apply for Leave
```
POST /leave-requests
Content-Type: application/json

{
  "employee_id": "uuid",
  "leave_type_id": "uuid",
  "start_date": "2024-02-01",
  "end_date": "2024-02-05",
  "reason": "Annual vacation"
}
```

#### List Leave Requests
```
GET /leave-requests?employee_id=uuid&status=pending
```

#### Get Leave Request
```
GET /leave-requests/{id}
```

#### Approve Leave Request
```
PUT /leave-requests/{id}/approve
Content-Type: application/json

{
  "approved_by": "manager-uuid"
}
```

#### Reject Leave Request
```
PUT /leave-requests/{id}/reject
Content-Type: application/json

{
  "rejection_reason": "Insufficient notice period"
}
```

#### Cancel Leave Request
```
PUT /leave-requests/{id}/cancel
```

### Audit Logs

#### Get Audit Logs
```
GET /audit-logs?table_name=employees&action=UPDATE&from=2024-01-01T00:00:00Z&to=2024-12-31T23:59:59Z&limit=50
```

**Filters Available**:
- `table_name`: Filter by table name
- `record_id`: Filter by specific record ID
- `action`: Filter by action (INSERT, UPDATE, DELETE)
- `changed_by`: Filter by user who made the change
- `from`: Start date (RFC3339 format)
- `to`: End date (RFC3339 format)
- `limit`: Number of records (default: 50, max: 200)

## 🚀 Installation & Setup

### Prerequisites
- Go 1.23 or higher
- PostgreSQL 12 or higher
- Git

### 1. Clone the Repository
```bash
git clone <repository-url>
cd lms
```

### 2. Set Up Database
```bash
# Connect to PostgreSQL
psql -U postgres -d your_database

# Run the schema
\i Database/db.sql
```

### 3. Install Dependencies
```bash
cd Backend
go mod tidy
```

### 4. Configure Environment
Create a `.env` file in the Backend directory:
```env
DATABASE_URL=postgresql://username:password@localhost:5432/lms_db
PORT=8080
```

### 5. Run the Application
```bash
go run main.go
```

The server will start on `http://localhost:8080`

## 🔧 Environment Variables

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `DATABASE_URL` | PostgreSQL connection string | - | ✅ |
| `PORT` | Server port | 8080 | ❌ |

## 📝 Usage Examples

### Complete Workflow Example

1. **Create a Leave Type**
```bash
curl -X POST http://localhost:8080/leave-types \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Annual Leave",
    "description": "Yearly vacation days",
    "max_days_per_year": 21,
    "carry_forward_allowed": true,
    "max_carry_forward_days": 5
  }'
```

2. **Create an Employee**
```bash
curl -X POST http://localhost:8080/employees \
  -H "Content-Type: application/json" \
  -d '{
    "name": "John Doe",
    "email": "john.doe@company.com",
    "department_id": "department-uuid",
    "joining_date": "2024-01-15"
  }'
```

3. **Apply for Leave**
```bash
curl -X POST http://localhost:8080/leave-requests \
  -H "Content-Type: application/json" \
  -d '{
    "employee_id": "employee-uuid",
    "leave_type_id": "leave-type-uuid",
    "start_date": "2024-02-01",
    "end_date": "2024-02-05",
    "reason": "Annual vacation"
  }'
```

4. **Approve Leave Request**
```bash
curl -X PUT http://localhost:8080/leave-requests/request-uuid/approve \
  -H "Content-Type: application/json" \
  -d '{
    "approved_by": "manager-uuid"
  }'
```

## ✅ Validation Rules

### Employee Creation
- ✅ Name and email are required
- ✅ Email must be unique
- ✅ Joining date cannot be in the future
- ✅ Department must exist
- ✅ Email format validation

### Leave Request
- ✅ Employee and leave type must exist
- ✅ Start date ≤ End date
- ✅ Dates must be in YYYY-MM-DD format
- ✅ No overlapping leave requests
- ✅ Sufficient leave balance available
- ✅ Start date ≥ employee joining date
- ✅ Reason is required

### Leave Type
- ✅ Name is required and unique
- ✅ Max days cannot be negative
- ✅ Carry forward days cannot be negative
- ✅ If carry forward not allowed, max carry forward days = 0

### Leave Balance
- ✅ Year must be between 2020-2050
- ✅ Days cannot be negative
- ✅ Employee must exist

## 🚨 Error Handling

### HTTP Status Codes
- `200` - Success
- `201` - Created
- `400` - Bad Request (validation errors)
- `404` - Not Found
- `500` - Internal Server Error

### Error Response Format
```json
{
  "error": "Error message",
  "details": "Additional error details"
}
```

### Common Error Messages
- `"name and email are required"`
- `"email already exists"`
- `"department_id not found"`
- `"joining_date cannot be in the future"`
- `"leave request overlaps with an existing request"`
- `"insufficient leave balance"`
- `"invalid date format, use YYYY-MM-DD"`

## 🧪 Testing

### Manual Testing with Postman

1. **Import the following collection structure**:
   - Health Check
   - Employee Management
   - Leave Types Management
   - Leave Requests Management
   - Audit Logs

2. **Test Scenarios**:
   - Create employee → Apply leave → Approve leave
   - Test validation errors
   - Test overlapping leave requests
   - Test insufficient balance scenarios

### Automated Testing
```bash
# Run tests
go test ./...

# Run with coverage
go test -cover ./...
```

## 🔒 Security Considerations

- **Input Validation**: All inputs are validated
- **SQL Injection Protection**: Parameterized queries used
- **Database Constraints**: Foreign key and check constraints
- **Audit Logging**: All changes are logged
- **Soft Deletes**: Data integrity maintained

## 📊 Performance Features

- **Connection Pooling**: Efficient database connections
- **Database Indexes**: Optimized query performance
- **Generated Columns**: Automatic calculations
- **Triggers**: Efficient audit logging

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a pull request


## 🆘 Support

For support and questions:
- Create an issue in the repository
- Check the API documentation
- Review the validation rules

---

**Built with ❤️ using Go and PostgreSQL**
