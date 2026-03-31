select u.id, u.name, u.email, count(o.id) as order_count, sum(o.total) as total_spent from users u left join orders o on o.user_id = u.id where u.created_at >= '2024-01-01' and u.status = 'active' group by u.id, u.name, u.email having count(o.id) > 0 order by total_spent desc limit 50;

create table if not exists products (id bigint generated always as identity primary key, name text not null, description text, price numeric(10, 2) not null check (price > 0), category_id integer references categories(id) on delete set null, created_at timestamptz not null default now(), updated_at timestamptz not null default now());

with monthly_revenue as (select date_trunc('month', o.created_at) as month, sum(o.total) as revenue from orders o where o.created_at >= now() - interval '12 months' group by 1) select month, revenue, revenue - lag(revenue) over (order by month) as change from monthly_revenue order by month;
