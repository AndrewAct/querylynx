-- QueryLynx Phase 0 — Tesla charging-station demo dataset (Supabase / PostgreSQL).
--
-- What this script does:
--   1. Creates a rich relational schema (enums, FKs, jsonb, timestamptz, partial indexes).
--   2. Seeds ~6,270 rows of synthetic-but-plausible data across 4 tables.
--   3. Creates a read-only role `querylynx_ro` that the MCP Executor connects as.
--
-- How to run:
--   - Supabase SQL Editor: paste the whole file and Run.
--   - or:  psql "$SUPABASE_DB_URL" -f deploy/supabase/setup.sql
--           ($SUPABASE_DB_URL must be the owner/superuser connection string — direct port 5432.)
--
-- The Executor then connects as `querylynx_ro`. NOTE: change the password below before
-- using anything beyond a throwaway demo project.
--
-- This script is idempotent: re-running it drops and rebuilds everything.

begin;

-- ---------------------------------------------------------------------------
-- Reset (idempotent)
-- ---------------------------------------------------------------------------
drop table if exists public.battery_telemetry cascade;
drop table if exists public.trips             cascade;
drop table if exists public.charging_sessions cascade;
drop table if exists public.vehicles          cascade;

drop type if exists public.charge_location_type;
drop type if exists public.vehicle_model;

-- Deterministic-ish seed so re-runs produce comparable data.
select setseed(0.42);

-- ---------------------------------------------------------------------------
-- Enums
-- ---------------------------------------------------------------------------
create type public.vehicle_model        as enum ('model_3', 'model_y', 'model_s', 'model_x');
create type public.charge_location_type as enum ('home', 'supercharger', 'destination', 'public');

-- ---------------------------------------------------------------------------
-- vehicles — the fleet (parent table)
-- ---------------------------------------------------------------------------
create table public.vehicles (
    id               bigint generated always as identity primary key,
    vin              text not null unique,
    model            public.vehicle_model not null,
    model_year       smallint not null check (model_year between 2012 and 2026),
    color            text not null,
    purchase_date    date not null,
    software_version text not null,
    created_at       timestamptz not null default now()
);
comment on table  public.vehicles       is 'Tesla fleet: one row per vehicle.';
comment on column public.vehicles.model is 'Vehicle model: model_3 | model_y | model_s | model_x.';

insert into public.vehicles (vin, model, model_year, color, purchase_date, software_version)
select
    'VIN' || lpad(g::text, 5, '0'),
    (array['model_3','model_y','model_s','model_x']::public.vehicle_model[])[1 + floor(random()*4)::int],
    (2019 + floor(random()*6))::smallint,
    (array['Pearl White','Solid Black','Midnight Silver','Deep Blue Metallic','Red Multi-Coat'])[1 + floor(random()*5)::int],
    date '2020-01-01' + (floor(random()*1800))::int,
    '2024.' || floor(random()*45)::text || '.' || floor(random()*10)::text
from generate_series(1, 30) g;

-- ---------------------------------------------------------------------------
-- charging_sessions — per-vehicle charging history (~15 per vehicle = ~450)
-- ---------------------------------------------------------------------------
create table public.charging_sessions (
    id            bigint generated always as identity primary key,
    vehicle_id    bigint not null references public.vehicles(id) on delete cascade,
    started_at    timestamptz not null,
    ended_at      timestamptz not null,
    location_name text not null,
    location_type public.charge_location_type not null,
    kwh_added     numeric(6,2) not null check (kwh_added >= 0),
    cost_usd      numeric(7,2) not null check (cost_usd >= 0),
    start_soc     smallint not null check (start_soc between 0 and 100),
    end_soc       smallint not null check (end_soc between 0 and 100),
    metadata      jsonb not null default '{}'::jsonb,
    check (ended_at >= started_at)
);
comment on table public.charging_sessions is 'Per-vehicle charging events: energy added, cost, location, SOC delta.';

insert into public.charging_sessions
    (vehicle_id, started_at, ended_at, location_name, location_type, kwh_added, cost_usd, start_soc, end_soc, metadata)
select
    v.id,
    t.started,
    t.started + make_interval(mins => (20 + floor(random()*70))::int),
    loc.name,
    loc.type::public.charge_location_type,
    round((10 + random()*60)::numeric, 2),
    round((2 + random()*25)::numeric, 2),
    t.s_soc,
    least(100, t.s_soc + (20 + floor(random()*40))::int)::smallint,
    jsonb_build_object(
        'connector', (array['CCS','NACS','Type2'])[1 + floor(random()*3)::int],
        'peak_kw',   round((50 + random()*200)::numeric, 1)
    )
from public.vehicles v
cross join generate_series(1, 15) g
cross join lateral (
    select
        now() - make_interval(days => floor(random()*60)::int, hours => floor(random()*24)::int) as started,
        floor(10 + random()*40)::int as s_soc
) t
cross join lateral (
    select name, type from (values
        ('Home Garage',            'home'),
        ('Supercharger - Fremont', 'supercharger'),
        ('Supercharger - Gilroy',  'supercharger'),
        ('Destination - Hotel',    'destination'),
        ('Public Lot - Downtown',  'public')
    ) as l(name, type)
    order by random()
    limit 1
) loc;

-- ---------------------------------------------------------------------------
-- trips — per-vehicle drives (~25 per vehicle = ~750)
-- ---------------------------------------------------------------------------
create table public.trips (
    id              bigint generated always as identity primary key,
    vehicle_id      bigint not null references public.vehicles(id) on delete cascade,
    started_at      timestamptz not null,
    ended_at        timestamptz not null,
    distance_km     numeric(7,1) not null check (distance_km >= 0),
    energy_used_kwh numeric(7,2) not null check (energy_used_kwh >= 0),
    avg_speed_kmh   numeric(5,1) not null check (avg_speed_kmh >= 0),
    max_speed_kmh   numeric(5,1) not null check (max_speed_kmh >= 0),
    start_location  text not null,
    end_location    text not null,
    metadata        jsonb not null default '{}'::jsonb,
    check (ended_at >= started_at)
);
comment on table public.trips is 'Per-vehicle drives: distance, energy used, speeds, endpoints.';

insert into public.trips
    (vehicle_id, started_at, ended_at, distance_km, energy_used_kwh, avg_speed_kmh, max_speed_kmh, start_location, end_location, metadata)
select
    v.id,
    t.started,
    t.started + make_interval(secs => (t.dist / t.avg_speed * 3600)::int),
    round(t.dist::numeric, 1),
    round((t.dist * (0.14 + random()*0.06))::numeric, 2),
    round(t.avg_speed::numeric, 1),
    round((t.avg_speed * (1.2 + random()*0.5))::numeric, 1),
    t.sl,
    t.el,
    jsonb_build_object('climate_on', random() > 0.5, 'passengers', (1 + floor(random()*4))::int)
from public.vehicles v
cross join generate_series(1, 25) g
cross join lateral (
    select
        now() - make_interval(days => floor(random()*30)::int, hours => floor(random()*24)::int) as started,
        (5 + random()*120)  as dist,
        (30 + random()*60)  as avg_speed,
        (array['Home','Office','Mall','Airport','Park','Gym'])[1 + floor(random()*6)::int] as sl,
        (array['Home','Office','Mall','Airport','Park','Gym'])[1 + floor(random()*6)::int] as el
) t;

-- ---------------------------------------------------------------------------
-- battery_telemetry — hourly readings for the last week (168 per vehicle = 5,040)
-- ---------------------------------------------------------------------------
create table public.battery_telemetry (
    id             bigint generated always as identity primary key,
    vehicle_id     bigint not null references public.vehicles(id) on delete cascade,
    recorded_at    timestamptz not null,
    soc_percent    smallint not null check (soc_percent between 0 and 100),
    battery_temp_c numeric(4,1) not null,
    voltage_v      numeric(5,1) not null,
    current_a      numeric(6,1) not null,
    range_km       numeric(5,1) not null check (range_km >= 0)
);
comment on table public.battery_telemetry is 'Hourly battery telemetry: SOC, temperature, voltage, current, estimated range.';

insert into public.battery_telemetry
    (vehicle_id, recorded_at, soc_percent, battery_temp_c, voltage_v, current_a, range_km)
select
    v.id,
    date_trunc('hour', now()) - make_interval(hours => h),
    (20 + floor(random()*75))::smallint,
    round((15 + random()*30)::numeric, 1),
    round((350 + random()*50)::numeric, 1),
    round((-20 + random()*250)::numeric, 1),
    round((150 + random()*350)::numeric, 1)
from public.vehicles v
cross join generate_series(0, 167) h;

-- ---------------------------------------------------------------------------
-- Indexes (incl. a partial index to mirror real-world schema shape)
-- ---------------------------------------------------------------------------
create index idx_sessions_vehicle    on public.charging_sessions (vehicle_id);
create index idx_sessions_started     on public.charging_sessions (started_at);
-- Partial index: supercharger sessions are the hot path for cost analysis.
create index idx_sessions_supercharger on public.charging_sessions (vehicle_id, started_at)
    where location_type = 'supercharger';
create index idx_trips_vehicle        on public.trips (vehicle_id, started_at);
create index idx_telemetry_vehicle    on public.battery_telemetry (vehicle_id, recorded_at);

-- ---------------------------------------------------------------------------
-- Read-only role for the MCP Executor (Invariant 1: the DB role cannot write).
-- ---------------------------------------------------------------------------
-- NOTE: Supabase's default database is `postgres`. Change the password below.
drop role if exists querylynx_ro;
create role querylynx_ro login password 'ro_password_change_me' noinherit;
grant connect on database postgres to querylynx_ro;
grant usage   on schema public   to querylynx_ro;
grant select  on all tables in schema public to querylynx_ro;
alter default privileges in schema public grant select on tables to querylynx_ro;

commit;

-- Quick sanity check (run separately if you like):
--   select 'vehicles' t, count(*) from public.vehicles
--   union all select 'charging_sessions', count(*) from public.charging_sessions
--   union all select 'trips',             count(*) from public.trips
--   union all select 'battery_telemetry', count(*) from public.battery_telemetry;
