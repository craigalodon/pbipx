{# reviewed with Example, Pat
select student_id
from {{ ref('student') }}
