{{ config(materialized='table') }}
-- formerly built from {{ ref('legacy_roster') }}
select s.student_id, e.course_code
from {{ ref('stg_student') }} s
join {{ source('raw', 'enrollment') }} e on e.student_id = s.student_id
