-- 0009_battery.sql — battery charge + wear health (laptops). Plain hardware
-- telemetry. NULL columns = unknown/not-a-laptop. present=false on desktops/VMs.
ALTER TABLE machine ADD COLUMN IF NOT EXISTS battery_present     boolean NOT NULL DEFAULT false;
ALTER TABLE machine ADD COLUMN IF NOT EXISTS battery_percent     int;
ALTER TABLE machine ADD COLUMN IF NOT EXISTS battery_ac          boolean NOT NULL DEFAULT false;
ALTER TABLE machine ADD COLUMN IF NOT EXISTS battery_charging    boolean NOT NULL DEFAULT false;
ALTER TABLE machine ADD COLUMN IF NOT EXISTS battery_health_pct  int;
ALTER TABLE machine ADD COLUMN IF NOT EXISTS battery_design_mwh  int;
ALTER TABLE machine ADD COLUMN IF NOT EXISTS battery_full_mwh    int;
ALTER TABLE machine ADD COLUMN IF NOT EXISTS battery_cycles      int;
ALTER TABLE machine ADD COLUMN IF NOT EXISTS battery_updated_at  timestamptz;
