{# reviewed with jane.doe@example.com #}
select student_id
from {{ ref('student') }}
