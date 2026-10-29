select
    extract(year from e.enrolled_on) as term_year,
    trim(leading from e.course_code) as course,
    substring(e.section_code from 1 for 2) as building,
    substr(e.section_code from 3) as room,
    overlay(e.course_code placing e.section_code from 2) as label
from warehouse.dbo.enrollment e
where e.course_code is not distinct from e.prior_course_code
