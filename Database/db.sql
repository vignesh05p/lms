-- Leave Management System Database Schema for Supabase
-- This schema handles employees, departments, leave types, and leave requests

-- Enable necessary extensions
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Create custom types/enums
CREATE TYPE leave_status AS ENUM ('pending', 'approved', 'rejected', 'cancelled');
CREATE TYPE employee_role AS ENUM ('employee', 'hr', 'manager', 'admin');

-- 1. Departments Table
CREATE TABLE departments (
    id UUID DEFAULT uuid_generate_v4() PRIMARY KEY,
    name VARCHAR(100) NOT NULL UNIQUE,
    description TEXT,
    manager_id UUID, -- Will be set after employees table is created
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- 2. Leave Types Table
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

-- 3. Employees Table
CREATE TABLE employees (
    id UUID DEFAULT uuid_generate_v4() PRIMARY KEY,
    employee_id VARCHAR(20) NOT NULL UNIQUE, -- Company employee ID
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
    CONSTRAINT check_email_format CHECK (email ~* '^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$')
);

-- 4. Employee Leave Balances Table
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

-- 5. Leave Requests Table
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

-- 6. Leave Request Conflicts Table (for tracking overlapping requests)
CREATE TABLE leave_conflicts (
    id UUID DEFAULT uuid_generate_v4() PRIMARY KEY,
    employee_id UUID NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    conflicting_request_id UUID NOT NULL REFERENCES leave_requests(id) ON DELETE CASCADE,
    conflict_start_date DATE NOT NULL,
    conflict_end_date DATE NOT NULL,
    resolved BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- 7. Audit Log Table (for tracking all changes)
CREATE TABLE audit_logs (
    id UUID DEFAULT uuid_generate_v4() PRIMARY KEY,
    table_name VARCHAR(50) NOT NULL,
    record_id UUID NOT NULL,
    action VARCHAR(20) NOT NULL, -- INSERT, UPDATE, DELETE
    old_values JSONB,
    new_values JSONB,
    changed_by UUID REFERENCES employees(id) ON DELETE SET NULL,
    changed_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Add foreign key constraint for department manager
ALTER TABLE departments 
ADD CONSTRAINT fk_departments_manager 
FOREIGN KEY (manager_id) REFERENCES employees(id) ON DELETE SET NULL;

-- Create indexes for better performance
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

CREATE INDEX idx_audit_logs_table_record ON audit_logs(table_name, record_id);
CREATE INDEX idx_audit_logs_changed_at ON audit_logs(changed_at);

-- Enable Row Level Security (RLS)
ALTER TABLE departments ENABLE ROW LEVEL SECURITY;
ALTER TABLE leave_types ENABLE ROW LEVEL SECURITY;
ALTER TABLE employees ENABLE ROW LEVEL SECURITY;
ALTER TABLE employee_leave_balances ENABLE ROW LEVEL SECURITY;
ALTER TABLE leave_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE leave_conflicts ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_logs ENABLE ROW LEVEL SECURITY;

-- RLS Policies

-- Departments: HR and Admins can see all, others can see their own department
CREATE POLICY "departments_select_policy" ON departments
    FOR SELECT USING (
        auth.jwt() ->> 'role' IN ('hr', 'admin') OR
        id IN (SELECT department_id FROM employees WHERE id = (auth.jwt() ->> 'user_id')::UUID)
    );

CREATE POLICY "departments_insert_policy" ON departments
    FOR INSERT WITH CHECK (auth.jwt() ->> 'role' IN ('hr', 'admin'));

CREATE POLICY "departments_update_policy" ON departments
    FOR UPDATE USING (auth.jwt() ->> 'role' IN ('hr', 'admin'));

-- Leave Types: Everyone can read, only HR/Admin can modify
CREATE POLICY "leave_types_select_policy" ON leave_types
    FOR SELECT USING (true);

CREATE POLICY "leave_types_insert_policy" ON leave_types
    FOR INSERT WITH CHECK (auth.jwt() ->> 'role' IN ('hr', 'admin'));

CREATE POLICY "leave_types_update_policy" ON leave_types
    FOR UPDATE USING (auth.jwt() ->> 'role' IN ('hr', 'admin'));

-- Employees: HR/Admin see all, managers see their team, employees see themselves
CREATE POLICY "employees_select_policy" ON employees
    FOR SELECT USING (
        auth.jwt() ->> 'role' IN ('hr', 'admin') OR
        id = (auth.jwt() ->> 'user_id')::UUID OR
        manager_id = (auth.jwt() ->> 'user_id')::UUID
    );

CREATE POLICY "employees_insert_policy" ON employees
    FOR INSERT WITH CHECK (auth.jwt() ->> 'role' IN ('hr', 'admin'));

CREATE POLICY "employees_update_policy" ON employees
    FOR UPDATE USING (
        auth.jwt() ->> 'role' IN ('hr', 'admin') OR
        id = (auth.jwt() ->> 'user_id')::UUID
    );

-- Leave Balances: HR/Admin see all, employees see their own
CREATE POLICY "leave_balances_select_policy" ON employee_leave_balances
    FOR SELECT USING (
        auth.jwt() ->> 'role' IN ('hr', 'admin') OR
        employee_id = (auth.jwt() ->> 'user_id')::UUID OR
        employee_id IN (SELECT id FROM employees WHERE manager_id = (auth.jwt() ->> 'user_id')::UUID)
    );

CREATE POLICY "leave_balances_insert_policy" ON employee_leave_balances
    FOR INSERT WITH CHECK (auth.jwt() ->> 'role' IN ('hr', 'admin'));

CREATE POLICY "leave_balances_update_policy" ON employee_leave_balances
    FOR UPDATE USING (auth.jwt() ->> 'role' IN ('hr', 'admin'));

-- Leave Requests: Employees see their own, managers see their team's, HR/Admin see all
CREATE POLICY "leave_requests_select_policy" ON leave_requests
    FOR SELECT USING (
        auth.jwt() ->> 'role' IN ('hr', 'admin') OR
        employee_id = (auth.jwt() ->> 'user_id')::UUID OR
        employee_id IN (SELECT id FROM employees WHERE manager_id = (auth.jwt() ->> 'user_id')::UUID)
    );

CREATE POLICY "leave_requests_insert_policy" ON leave_requests
    FOR INSERT WITH CHECK (
        employee_id = (auth.jwt() ->> 'user_id')::UUID OR
        auth.jwt() ->> 'role' IN ('hr', 'admin')
    );

CREATE POLICY "leave_requests_update_policy" ON leave_requests
    FOR UPDATE USING (
        auth.jwt() ->> 'role' IN ('hr', 'admin') OR
        (employee_id = (auth.jwt() ->> 'user_id')::UUID AND status = 'pending') OR
        (employee_id IN (SELECT id FROM employees WHERE manager_id = (auth.jwt() ->> 'user_id')::UUID) AND auth.jwt() ->> 'role' = 'manager')
    );

-- Audit Logs: Only HR/Admin can see
CREATE POLICY "audit_logs_select_policy" ON audit_logs
    FOR SELECT USING (auth.jwt() ->> 'role' IN ('hr', 'admin'));

-- Functions for business logic

-- Function to calculate working days between two dates (excluding weekends)
CREATE OR REPLACE FUNCTION calculate_working_days(start_date DATE, end_date DATE)
RETURNS INTEGER AS $$
DECLARE
    total_days INTEGER;
    weekends INTEGER;
BEGIN
    total_days := end_date - start_date + 1;
    
    -- Calculate weekends in the range
    weekends := (
        SELECT COUNT(*)
        FROM generate_series(start_date, end_date, '1 day'::interval) AS date_series
        WHERE EXTRACT(DOW FROM date_series) IN (0, 6) -- Sunday = 0, Saturday = 6
    );
    
    RETURN total_days - weekends;
END;
$$ LANGUAGE plpgsql;

-- Function to check for overlapping leave requests
CREATE OR REPLACE FUNCTION check_leave_overlap(
    p_employee_id UUID,
    p_start_date DATE,
    p_end_date DATE,
    p_exclude_request_id UUID DEFAULT NULL
)
RETURNS BOOLEAN AS $$
DECLARE
    overlap_count INTEGER;
BEGIN
    SELECT COUNT(*)
    INTO overlap_count
    FROM leave_requests
    WHERE employee_id = p_employee_id
    AND status IN ('approved', 'pending')
    AND (p_exclude_request_id IS NULL OR id != p_exclude_request_id)
    AND (
        (start_date <= p_end_date AND end_date >= p_start_date)
    );
    
    RETURN overlap_count > 0;
END;
$$ LANGUAGE plpgsql;

-- Function to get available leave balance
CREATE OR REPLACE FUNCTION get_available_balance(
    p_employee_id UUID,
    p_leave_type_id UUID,
    p_year INTEGER DEFAULT EXTRACT(YEAR FROM CURRENT_DATE)
)
RETURNS INTEGER AS $$
DECLARE
    balance INTEGER;
BEGIN
    SELECT available_days
    INTO balance
    FROM employee_leave_balances
    WHERE employee_id = p_employee_id
    AND leave_type_id = p_leave_type_id
    AND year = p_year;
    
    RETURN COALESCE(balance, 0);
END;
$$ LANGUAGE plpgsql;

-- Trigger function for audit logging
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

-- Create audit triggers
CREATE TRIGGER employees_audit_trigger
    AFTER INSERT OR UPDATE OR DELETE ON employees
    FOR EACH ROW EXECUTE FUNCTION audit_trigger_function();

CREATE TRIGGER leave_requests_audit_trigger
    AFTER INSERT OR UPDATE OR DELETE ON leave_requests
    FOR EACH ROW EXECUTE FUNCTION audit_trigger_function();

CREATE TRIGGER leave_balances_audit_trigger
    AFTER INSERT OR UPDATE OR DELETE ON employee_leave_balances
    FOR EACH ROW EXECUTE FUNCTION audit_trigger_function();

-- Trigger to update timestamps
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create update triggers for all tables
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

-- Insert default data

-- Default departments
INSERT INTO departments (name, description) VALUES
('Human Resources', 'Manages employee relations and policies'),
('Engineering', 'Software development and technical operations'),
('Marketing', 'Brand promotion and customer acquisition'),
('Sales', 'Revenue generation and client relations'),
('Finance', 'Financial planning and accounting');

-- Default leave types
INSERT INTO leave_types (name, description, max_days_per_year, carry_forward_allowed, max_carry_forward_days) VALUES
('Annual Leave', 'Yearly vacation days', 21, true, 5),
('Sick Leave', 'Medical leave for illness', 12, false, 0),
('Maternity Leave', 'Leave for new mothers', 90, false, 0),
('Paternity Leave', 'Leave for new fathers', 15, false, 0),
('Emergency Leave', 'Urgent personal matters', 5, false, 0),
('Compensatory Leave', 'Time off for overtime work', 10, true, 3);

-- Grant permissions (adjust based on your auth setup)
GRANT USAGE ON SCHEMA public TO authenticated;
GRANT ALL ON ALL TABLES IN SCHEMA public TO authenticated;
GRANT ALL ON ALL SEQUENCES IN SCHEMA public TO authenticated;
GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO authenticated;