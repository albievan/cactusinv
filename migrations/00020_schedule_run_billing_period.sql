-- +goose Up
-- Each run records the billing period [period_start, period_end) it covered, so a schedule
-- never bills any stretch of time twice: a run overlapping a billed period is refused, and
-- an edited or resumed schedule continues after the latest billed period (M21). Periods are
-- anchored on the schedule's start date (domain.Recurrence.BillingPeriod): runs of
-- interval x 1, x 3 or x 12 months from the start month; runs of interval weeks from the
-- Monday of the start week; one day for a once schedule. Existing runs are backfilled under
-- the schedule's current recurrence.
ALTER TABLE schedule_runs ADD COLUMN period_start DATE NULL, ADD COLUMN period_end DATE NULL;

UPDATE schedule_runs r
JOIN (SELECT id, start_date,
             YEAR(start_date) * 12 + MONTH(start_date) - 1 AS anchor,
             CASE frequency WHEN 'quarterly' THEN 3 WHEN 'yearly' THEN 12 ELSE 1 END * GREATEST(interval_count, 1) AS step
      FROM schedules WHERE frequency IN ('monthly', 'quarterly', 'yearly')) s ON s.id = r.schedule_id
SET r.period_start = DATE_ADD(MAKEDATE(YEAR(s.start_date), 1),
                       INTERVAL MONTH(s.start_date) - 1 + FLOOR((YEAR(r.period) * 12 + MONTH(r.period) - 1 - s.anchor) / s.step) * s.step MONTH);
UPDATE schedule_runs r
JOIN (SELECT id, CASE frequency WHEN 'quarterly' THEN 3 WHEN 'yearly' THEN 12 ELSE 1 END * GREATEST(interval_count, 1) AS step
      FROM schedules WHERE frequency IN ('monthly', 'quarterly', 'yearly')) s ON s.id = r.schedule_id
SET r.period_end = r.period_start + INTERVAL s.step MONTH;

UPDATE schedule_runs r
JOIN (SELECT id, start_date - INTERVAL WEEKDAY(start_date) DAY AS anchor, 7 * GREATEST(interval_count, 1) AS len
      FROM schedules WHERE frequency = 'weekly') s ON s.id = r.schedule_id
SET r.period_start = DATE_ADD(s.anchor, INTERVAL FLOOR(DATEDIFF(r.period - INTERVAL WEEKDAY(r.period) DAY, s.anchor) / s.len) * s.len DAY);
UPDATE schedule_runs r
JOIN (SELECT id, 7 * GREATEST(interval_count, 1) AS len FROM schedules WHERE frequency = 'weekly') s ON s.id = r.schedule_id
SET r.period_end = r.period_start + INTERVAL s.len DAY;

UPDATE schedule_runs SET period_start = period, period_end = period + INTERVAL 1 DAY WHERE period_start IS NULL;

-- A period billed twice before (allowed until 00017, or under another recurrence) keeps its
-- first run; the others cover only their own day, so history is kept and keys stay unique.
UPDATE schedule_runs r
JOIN (SELECT schedule_id, period_start, MIN(period) AS first_period FROM schedule_runs
      GROUP BY schedule_id, period_start HAVING COUNT(*) > 1) d
  ON d.schedule_id = r.schedule_id AND d.period_start = r.period_start AND r.period <> d.first_period
SET r.period_start = r.period, r.period_end = r.period + INTERVAL 1 DAY;

-- The billing period is the run's identity; period_key stays as its label. Two runs with
-- one label need not overlap (a recurrence edited after an old run), so it is not unique.
ALTER TABLE schedule_runs MODIFY period_start DATE NOT NULL, MODIFY period_end DATE NOT NULL,
    DROP PRIMARY KEY, ADD PRIMARY KEY (schedule_id, period_start), ADD KEY schedule_runs_period_key (schedule_id, period_key);

-- +goose Down
-- Back to one run per label: a label used twice keeps its first run, the others their date.
UPDATE schedule_runs r
JOIN (SELECT schedule_id, period_key, MIN(period) AS first_period FROM schedule_runs
      GROUP BY schedule_id, period_key HAVING COUNT(*) > 1) d
  ON d.schedule_id = r.schedule_id AND d.period_key = r.period_key AND r.period <> d.first_period
SET r.period_key = DATE_FORMAT(r.period, '%Y-%m-%d');
ALTER TABLE schedule_runs DROP PRIMARY KEY, DROP KEY schedule_runs_period_key, ADD PRIMARY KEY (schedule_id, period_key),
    DROP COLUMN period_start, DROP COLUMN period_end;
