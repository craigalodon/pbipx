select *
from {{ ref(var('roster_model')) }} r
join {{ source(schema_name, 'student') }} s on s.student_id = r.student_id
