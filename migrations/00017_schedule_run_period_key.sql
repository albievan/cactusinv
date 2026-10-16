-- +goose Up
-- A schedule raises at most one invoice per billing PERIOD (month, calendar quarter, year;
-- the date for weekly and once), not per nominal date: moving the day of the month or the
-- start date after a run must not bill the same period twice.
ALTER TABLE schedule_runs ADD COLUMN period_key VARCHAR(10) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '';
UPDATE schedule_runs r JOIN schedules s ON s.id = r.schedule_id
SET r.period_key = CASE s.frequency
    WHEN 'monthly' THEN DATE_FORMAT(r.period, '%Y-%m')
    WHEN 'quarterly' THEN CONCAT(YEAR(r.period), '-Q', QUARTER(r.period))
    WHEN 'yearly' THEN CAST(YEAR(r.period) AS CHAR)
    ELSE DATE_FORMAT(r.period, '%Y-%m-%d')
END;
-- A period already billed twice (the old key allowed it) keeps its first run under the
-- period key; the others keep their date as key, so history is preserved.
UPDATE schedule_runs r
JOIN (SELECT schedule_id, period_key, MIN(period) AS first_period FROM schedule_runs
      GROUP BY schedule_id, period_key HAVING COUNT(*) > 1) d
  ON d.schedule_id = r.schedule_id AND d.period_key = r.period_key AND r.period <> d.first_period
SET r.period_key = DATE_FORMAT(r.period, '%Y-%m-%d');
ALTER TABLE schedule_runs DROP PRIMARY KEY, ADD PRIMARY KEY (schedule_id, period_key), ADD KEY schedule_runs_period (schedule_id, period);

-- +goose Down
ALTER TABLE schedule_runs DROP PRIMARY KEY, DROP KEY schedule_runs_period, ADD PRIMARY KEY (schedule_id, period);
ALTER TABLE schedule_runs DROP COLUMN period_key;
