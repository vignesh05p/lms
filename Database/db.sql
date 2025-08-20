-- ==========================================
-- Leave Management System (Supabase + Go)
-- ==========================================

-- Drop existing schema (careful: deletes data)
DROP SCHEMA public CASCADE;
CREATE SCHEMA public;

-- Enable extensions
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Enums
CREATE TYPE leave_status AS ENUM ('pending', 'approved', 'rejected', 'cancelled');
CREATE TYPE employee_role AS ENUM ('employee', 'hr', 'manager', 'admin');

-- 1. Departments
CREATE TABLE departments (
    id UUID DEFAULT uuid_generate_v4() PRIMARY KEY,
    name VARCHAR(100) NOT NULL UNIQUE,
    description TEXT,
    manager_id UUID,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);


-- 2. Leave Types
CREATE TABLE leave_types (
    id UUID DEFAULT uuid_generate_v4() PRIMARY KEY,
    name VARCHAR(50) NOT NULL UNIQUE,
    description TEXT,
    max_days_per_year INTEGER NOT NULL DEFAULT 0,
    carry_forward_allowed BOOLEAN DEFAULT FALSE,
    max_carry_forward_days INTEGER DEFAULT 0,
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    CONSTRAINT check_max_days_positive CHECK (max_days_per_year >= 0),
    CONSTRAINT check_carry_forward_days CHECK (max_carry_forward_days >= 0)
);

-- 3. Employees
CREATE TABLE employees (
    id UUID DEFAULT uuid_generate_v4() PRIMARY KEY,
    employee_id VARCHAR(20) NOT NULL UNIQUE,
    email VARCHAR(255) NOT NULL UNIQUE,
    name VARCHAR(255) NOT NULL,
    department_id UUID NOT NULL REFERENCES departments(id) ON DELETE RESTRICT,
    role employee_role DEFAULT 'employee',
    joining_date DATE NOT NULL,
    manager_id UUID REFERENCES employees(id) ON DELETE SET NULL,
    is_active BOOLEAN DEFAULT TRUE,
    phone VARCHAR(15),
    address TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    CONSTRAINT check_joining_date CHECK (joining_date <= CURRENT_DATE),
    CONSTRAINT check_email_format CHECK (email ~* '^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$'),
    CONSTRAINT check_phone_format CHECK (phone IS NULL OR phone ~ '^\+?[0-9]{7,15}$')
);

-- 4. Employee Leave Balances
CREATE TABLE employee_leave_balances (
    id UUID DEFAULT uuid_generate_v4() PRIMARY KEY,
    employee_id UUID NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    leave_type_id UUID NOT NULL REFERENCES leave_types(id) ON DELETE CASCADE,
    year INTEGER NOT NULL,
    allocated_days INTEGER NOT NULL DEFAULT 0,
    used_days INTEGER NOT NULL DEFAULT 0,
    carried_forward_days INTEGER NOT NULL DEFAULT 0,
    available_days INTEGER GENERATED ALWAYS AS (allocated_days + carried_forward_days - used_days) STORED,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(employee_id, leave_type_id, year),
    CONSTRAINT check_allocated_days_positive CHECK (allocated_days >= 0),
    CONSTRAINT check_used_days_positive CHECK (used_days >= 0),
    CONSTRAINT check_carried_forward_positive CHECK (carried_forward_days >= 0),
    CONSTRAINT check_used_days_limit CHECK (used_days <= allocated_days + carried_forward_days),
    CONSTRAINT check_year_valid CHECK (year >= 2020 AND year <= 2050)
);

-- 5. Leave Requests
CREATE TABLE leave_requests (
    id UUID DEFAULT uuid_generate_v4() PRIMARY KEY,
    employee_id UUID NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    leave_type_id UUID NOT NULL REFERENCES leave_types(id) ON DELETE RESTRICT,
    start_date DATE NOT NULL,
    end_date DATE NOT NULL,
    total_days INTEGER NOT NULL,
    reason TEXT NOT NULL,
    status leave_status DEFAULT 'pending',
    applied_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    approved_by UUID REFERENCES employees(id) ON DELETE SET NULL,
    approved_at TIMESTAMP WITH TIME ZONE,
    rejection_reason TEXT,
    comments TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    CONSTRAINT check_date_order CHECK (end_date >= start_date),
    CONSTRAINT check_total_days_positive CHECK (total_days > 0),
    CONSTRAINT check_reason_not_empty CHECK (LENGTH(TRIM(reason)) > 0),
    CONSTRAINT check_approved_status CHECK (
        (status = 'approved' AND approved_by IS NOT NULL AND approved_at IS NOT NULL) OR
        (status != 'approved')
    )
);

-- 6. Leave Conflicts
CREATE TABLE leave_conflicts (
    id UUID DEFAULT uuid_generate_v4() PRIMARY KEY,
    employee_id UUID NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    conflicting_request_id UUID NOT NULL REFERENCES leave_requests(id) ON DELETE CASCADE,
    conflict_start_date DATE NOT NULL,
    conflict_end_date DATE NOT NULL,
    resolved BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- 7. Audit Logs
CREATE TABLE audit_logs (
    id UUID DEFAULT uuid_generate_v4() PRIMARY KEY,
    table_name VARCHAR(50) NOT NULL,
    record_id UUID NOT NULL,
    action VARCHAR(20) NOT NULL,
    old_values JSONB,
    new_values JSONB,
    changed_by UUID REFERENCES employees(id) ON DELETE SET NULL,
    changed_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Indexes
CREATE INDEX idx_employees_department ON employees(department_id);
CREATE INDEX idx_employees_manager ON employees(manager_id);
CREATE INDEX idx_employees_email ON employees(email);
CREATE INDEX idx_employees_employee_id ON employees(employee_id);
CREATE INDEX idx_employees_joining_date ON employees(joining_date);
CREATE INDEX idx_leave_requests_employee ON leave_requests(employee_id);
CREATE INDEX idx_leave_requests_dates ON leave_requests(start_date, end_date);
CREATE INDEX idx_leave_requests_status ON leave_requests(status);
CREATE INDEX idx_leave_requests_leave_type ON leave_requests(leave_type_id);
CREATE INDEX idx_leave_balances_employee_year ON employee_leave_balances(employee_id, year);
CREATE INDEX idx_leave_balances_leave_type ON employee_leave_balances(leave_type_id);
CREATE INDEX idx_leave_balances_year ON employee_leave_balances(year);
CREATE INDEX idx_audit_logs_table_record ON audit_logs(table_name, record_id);
CREATE INDEX idx_audit_logs_changed_at ON audit_logs(changed_at);

-- Triggers & Functions

-- Auto employee_id
CREATE OR REPLACE FUNCTION generate_employee_id()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.employee_id IS NULL THEN
        NEW.employee_id := 'EMP-' || TO_CHAR(NOW(), 'YYYYMMDD-HH24MISS');
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_generate_employee_id
BEFORE INSERT ON employees
FOR EACH ROW EXECUTE FUNCTION generate_employee_id();

-- Auto leave balance for new employee
CREATE OR REPLACE FUNCTION init_leave_balances()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO employee_leave_balances (employee_id, leave_type_id, year, allocated_days)
    SELECT NEW.id, lt.id, EXTRACT(YEAR FROM CURRENT_DATE)::INT, lt.max_days_per_year
    FROM leave_types lt
    WHERE lt.is_active = true;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_init_leave_balances
AFTER INSERT ON employees
FOR EACH ROW EXECUTE FUNCTION init_leave_balances();

-- Audit Trigger
CREATE OR REPLACE FUNCTION audit_trigger_function()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        INSERT INTO audit_logs (table_name, record_id, action, new_values)
        VALUES (TG_TABLE_NAME, NEW.id, 'INSERT', row_to_json(NEW));
        RETURN NEW;
    ELSIF TG_OP = 'UPDATE' THEN
        INSERT INTO audit_logs (table_name, record_id, action, old_values, new_values)
        VALUES (TG_TABLE_NAME, NEW.id, 'UPDATE', row_to_json(OLD), row_to_json(NEW));
        RETURN NEW;
    ELSIF TG_OP = 'DELETE' THEN
        INSERT INTO audit_logs (table_name, record_id, action, old_values)
        VALUES (TG_TABLE_NAME, OLD.id, 'DELETE', row_to_json(OLD));
        RETURN OLD;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER employees_audit_trigger AFTER INSERT OR UPDATE OR DELETE ON employees
    FOR EACH ROW EXECUTE FUNCTION audit_trigger_function();
CREATE TRIGGER leave_requests_audit_trigger AFTER INSERT OR UPDATE OR DELETE ON leave_requests
    FOR EACH ROW EXECUTE FUNCTION audit_trigger_function();
CREATE TRIGGER leave_balances_audit_trigger AFTER INSERT OR UPDATE OR DELETE ON employee_leave_balances
    FOR EACH ROW EXECUTE FUNCTION audit_trigger_function();

-- Utility functions required by backend

-- Calculate working days (Mon-Fri) between two dates inclusive
CREATE OR REPLACE FUNCTION calculate_working_days(start_date DATE, end_date DATE)
RETURNS INTEGER AS $$
    SELECT COUNT(*)::INT
    FROM generate_series(start_date, end_date, interval '1 day') AS d(day)
    WHERE EXTRACT(ISODOW FROM d.day) < 6; -- 1..5 are Mon..Fri
$$ LANGUAGE SQL STABLE;

-- Check overlapping leave requests for an employee, optionally excluding a specific request
CREATE OR REPLACE FUNCTION check_leave_overlap(
    p_employee_id UUID,
    p_start_date DATE,
    p_end_date DATE,
    p_exclude_request_id UUID
)
RETURNS BOOLEAN AS $$
    SELECT EXISTS (
        SELECT 1
        FROM leave_requests lr
        WHERE lr.employee_id = p_employee_id
          AND lr.status IN ('pending','approved')
          AND (p_exclude_request_id IS NULL OR lr.id <> p_exclude_request_id)
          AND NOT (lr.end_date < p_start_date OR lr.start_date > p_end_date)
    );
$$ LANGUAGE SQL STABLE;

-- Update updated_at
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER update_departments_updated_at BEFORE UPDATE ON departments
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER update_leave_types_updated_at BEFORE UPDATE ON leave_types
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER update_employees_updated_at BEFORE UPDATE ON employees
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER update_leave_balances_updated_at BEFORE UPDATE ON employee_leave_balances
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER update_leave_requests_updated_at BEFORE UPDATE ON leave_requests
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Default seed data
INSERT INTO departments (name, description) VALUES
('Human Resources', 'Manages employee relations and policies'),
('Engineering', 'Software development and technical operations'),
('Marketing', 'Brand promotion and customer acquisition'),
('Sales', 'Revenue generation and client relations'),
('Finance', 'Financial planning and accounting');

INSERT INTO leave_types (name, description, max_days_per_year, carry_forward_allowed, max_carry_forward_days) VALUES
('Annual Leave', 'Yearly vacation days', 21, true, 5),
('Sick Leave', 'Medical leave for illness', 12, false, 0),
('Maternity Leave', 'Leave for new mothers', 90, false, 0),
('Paternity Leave', 'Leave for new fathers', 15, false, 0),
('Emergency Leave', 'Urgent personal matters', 5, false, 0),
('Compensatory Leave', 'Time off for overtime work', 10, true, 3);
