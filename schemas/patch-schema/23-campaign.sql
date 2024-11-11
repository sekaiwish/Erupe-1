BEGIN;

CREATE TABLE IF NOT EXISTS public.campaigns (
  id SERIAL PRIMARY KEY,
  min_hr INTEGER,
  max_hr INTEGER,
  min_sr INTEGER,
  max_sr INTEGER,
  min_gr INTEGER,
  max_gr INTEGER,
  reward_type INTEGER,
  stamps INTEGER,
  unk INTEGER,
  background_id INTEGER,
  start_time TIMESTAMP WITH TIME ZONE,
  end_time TIMESTAMP WITH TIME ZONE,
  title TEXT,
  reward TEXT,
  link TEXT,
  code_prefix TEXT
);

CREATE TABLE IF NOT EXISTS public.campaign_categories (
  id SERIAL PRIMARY KEY,
  type INTEGER,
  title TEXT,
  description TEXT
);

CREATE TABLE IF NOT EXISTS public.campaign_category_links (
  id SERIAL PRIMARY KEY,
  campaign_id INTEGER,
  category_id INTEGER
);

CREATE TABLE IF NOT EXISTS public.campaign_rewards (
  id SERIAL PRIMARY KEY,
  campaign_id INTEGER,
  item_type INTEGER,
  quantity INTEGER,
  item_id INTEGER
);

CREATE TABLE IF NOT EXISTS public.campaign_rewards_claimed (
  character_id INTEGER,
  reward_id INTEGER
);

CREATE TABLE IF NOT EXISTS public.campaign_state (
  id SERIAL PRIMARY KEY,
  campaign_id INTEGER,
  character_id INTEGER,
  code TEXT
);

CREATE TABLE IF NOT EXISTS public.campaign_codes (
  code TEXT,
  multi BOOLEAN
);

CREATE TABLE IF NOT EXISTS public.campaign_quest (
  campaign_id INTEGER,
  character_id INTEGER
);

END;