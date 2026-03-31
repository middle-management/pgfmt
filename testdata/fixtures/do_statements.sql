DO $$
DECLARE
  x integer := 0;
BEGIN
  x := x + 1;
  RAISE NOTICE 'x is %', x;
END;
$$;

DO $$
DECLARE
  source_trigger_row RECORD;
  new_workflow_id uuid;
  migrated_count integer := 0;
BEGIN
  FOR source_trigger_row IN
    SELECT
      st.source_trigger_id,
      st.workflow_id,
      w.name AS workflow_name
    FROM studio.source_trigger st
    JOIN studio.workflow w
      ON w.workflow_id = st.workflow_id
    WHERE EXISTS (
      SELECT 1
      FROM studio.workflow_step ws
      WHERE ws.workflow_id = st.workflow_id
    )
  LOOP
    new_workflow_id := studio_internal.generate_id('workflow');

    INSERT INTO studio.workflow (workflow_id, name)
    VALUES (new_workflow_id, source_trigger_row.workflow_name);

    UPDATE studio.source_trigger
    SET workflow_id = new_workflow_id
    WHERE source_trigger_id = source_trigger_row.source_trigger_id;

    RAISE NOTICE 'migrated source_trigger %', source_trigger_row.source_trigger_id;

    migrated_count := migrated_count + 1;
  END LOOP;

  RAISE NOTICE 'migrated % source trigger(s)', migrated_count;

  IF EXISTS (
    SELECT 1
    FROM studio.source_trigger st
    JOIN studio.workflow_trigger wt
      ON wt.source_trigger_id = st.source_trigger_id
    WHERE st.workflow_id <> wt.workflow_id
  ) THEN
    RAISE EXCEPTION 'workflow_ids are out of sync';
  END IF;
END;
$$;
