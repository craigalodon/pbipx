{{ config(materialized='view', meta={'owner': 'jane.doe@example.com'}) }}

select section_number, count(*) as students
from {{ ref('enrollment') }}
group by section_number
