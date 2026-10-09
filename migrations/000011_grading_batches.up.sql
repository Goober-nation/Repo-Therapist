-- Batch regrade becomes batch grading (docs/admin.md): the tables are renamed, their data kept.
ALTER TABLE regrade_batches RENAME TO grading_batches;
ALTER TABLE regrade_batch_items RENAME TO grading_batch_items;

ALTER SEQUENCE regrade_batches_id_seq RENAME TO grading_batches_id_seq;
ALTER INDEX regrade_batches_pkey RENAME TO grading_batches_pkey;
ALTER INDEX regrade_batches_one_active RENAME TO grading_batches_one_active;
ALTER INDEX regrade_batch_items_pkey RENAME TO grading_batch_items_pkey;
ALTER INDEX regrade_batch_items_batch_id_status_idx RENAME TO grading_batch_items_batch_id_status_idx;
ALTER TABLE grading_batches RENAME CONSTRAINT regrade_batches_status_check TO grading_batches_status_check;
ALTER TABLE grading_batch_items RENAME CONSTRAINT regrade_batch_items_status_check TO grading_batch_items_status_check;
ALTER TABLE grading_batch_items RENAME CONSTRAINT regrade_batch_items_batch_id_fkey TO grading_batch_items_batch_id_fkey;
ALTER TABLE grading_batch_items RENAME CONSTRAINT regrade_batch_items_repo_id_fkey TO grading_batch_items_repo_id_fkey;
