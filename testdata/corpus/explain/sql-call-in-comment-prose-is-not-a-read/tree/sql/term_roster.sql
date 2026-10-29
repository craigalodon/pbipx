-- checked with source('Jane', 'Doe') on Monday
-- depends_on: {{ ref('stg_term') }}
select 'see ref(''Roe'')' as note, student_id
from warehouse.dbo.enrollment
