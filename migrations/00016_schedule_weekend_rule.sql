-- +goose Up
-- A day-of-month schedule whose day falls on a weekend can run on the Friday before
-- (previous-friday). Existing schedules keep running on the day itself (none); new ones
-- default to previous-friday in the application.
ALTER TABLE schedules ADD COLUMN weekend_rule VARCHAR(16) NOT NULL DEFAULT 'none';
ALTER TABLE schedules ADD CONSTRAINT schedules_weekend_rule CHECK (weekend_rule IN ('none', 'previous-friday'));

-- +goose Down
ALTER TABLE schedules DROP CONSTRAINT schedules_weekend_rule;
ALTER TABLE schedules DROP COLUMN weekend_rule;
