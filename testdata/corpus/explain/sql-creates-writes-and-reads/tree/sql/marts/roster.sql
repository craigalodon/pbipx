create or replace view analytics.roster as
with active as (
    select student_id, full_name
    from warehouse.dbo.enrollment
    where full_name <> 'Doe, Jane'
)
select a.student_id, s.cohort_year
from active a
join warehouse.dbo.student s on s.student_id = a.student_id;

insert into analytics.roster_log
select student_id, 1 from staging_roster, warehouse.dbo.section sec;

merge into analytics.student t
using warehouse.dbo.student_update u
on t.student_id = u.student_id
when matched then update set t.cohort_year = u.cohort_year;
