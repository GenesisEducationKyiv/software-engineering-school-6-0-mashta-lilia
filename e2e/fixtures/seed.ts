// Helpers for inserting tracked-repo and subscription rows directly into the
// e2e Postgres before each test. We bypass the HTTP API on purpose:
// Subscribe requires a real GitHub round-trip, while the user journeys we
// want to drive end-to-end are the *links from the email* (confirm /
// unsubscribe). Seeding gives us a known token.
import { Client } from 'pg';

export interface DbConfig {
  host: string;
  port: number;
  user: string;
  password: string;
  database: string;
}

export const defaultDbConfig: DbConfig = {
  host: process.env.E2E_DB_HOST ?? 'localhost',
  port: parseInt(process.env.E2E_DB_PORT ?? '5433', 10),
  user: process.env.E2E_DB_USER ?? 'postgres',
  password: process.env.E2E_DB_PASSWORD ?? 'postgres',
  database: process.env.E2E_DB_NAME ?? 'release_notifier',
};

export async function withDb<T>(
  fn: (client: Client) => Promise<T>,
  cfg: DbConfig = defaultDbConfig,
): Promise<T> {
  const client = new Client(cfg);
  await client.connect();
  try {
    return await fn(client);
  } finally {
    await client.end();
  }
}

export async function reset(cfg: DbConfig = defaultDbConfig): Promise<void> {
  await withDb(async (c) => {
    await c.query('TRUNCATE subscriptions, tracked_repositories CASCADE');
  }, cfg);
}

export interface SeedOptions {
  email: string;
  owner: string;
  name: string;
  token: string;
  status: 'pending' | 'active' | 'unsubscribed';
}

/**
 * Seeds a tracked repo + subscription in one transaction. Returns the
 * generated subscription id so tests can assert on the row afterwards.
 */
export async function seedSubscription(
  opts: SeedOptions,
  cfg: DbConfig = defaultDbConfig,
): Promise<number> {
  return withDb(async (c) => {
    await c.query('BEGIN');
    try {
      await c.query(
        `INSERT INTO tracked_repositories (owner, name)
         VALUES ($1, $2)
         ON CONFLICT (owner, name) DO NOTHING`,
        [opts.owner, opts.name],
      );

      const insert = await c.query(
        `INSERT INTO subscriptions (email, repo_owner, repo_name, token, status)
         VALUES ($1, $2, $3, $4, $5)
         RETURNING id`,
        [opts.email, opts.owner, opts.name, opts.token, opts.status],
      );

      await c.query('COMMIT');
      return insert.rows[0].id as number;
    } catch (err) {
      await c.query('ROLLBACK');
      throw err;
    }
  }, cfg);
}

export async function statusOf(
  token: string,
  cfg: DbConfig = defaultDbConfig,
): Promise<string | null> {
  return withDb(async (c) => {
    const r = await c.query('SELECT status FROM subscriptions WHERE token = $1', [token]);
    if (r.rowCount === 0) return null;
    return r.rows[0].status as string;
  }, cfg);
}
