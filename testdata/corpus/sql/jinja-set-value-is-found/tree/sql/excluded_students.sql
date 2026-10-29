{% set excluded_name = 'Doe, Jane' %}

select student_id
from {{ ref('student') }}
