-- Complete the agent field retirements from #5017, #5062 and #5164.
-- Nested concept blocks are closed schemas too. The 20260908 migrations
-- repaired top-level conversational controls but left legacy capabilities
-- and provider settings that reject the next read-merge. Production upgrade
-- inspection found these on all nine current agents, including policyName
-- on three. Repair every history version, preserving declared siblings.
--
-- The remaining avatar fields and old planner pointer were removed in the
-- same source changes. A plan id is not a work run id: do not rename it into
-- a relationship to unrelated data. These removals require a pre-upgrade
-- backup to restore their values; the down migration cannot invent them.

UPDATE "MemoryNodes"
SET payload = payload - 'avatar' - 'avatarPersonaId' - 'avatarVendor'
WHERE concept = 'v1:agents:agent'
  AND payload ?| array['avatar', 'avatarPersonaId', 'avatarVendor'];

UPDATE "MemoryNodes"
SET payload = payload
  #- '{capabilities,avatar}'
  #- '{capabilities,lipSync}'
  #- '{capabilities,vision}'
  #- '{capabilities,voiceToVoice}'
  #- '{capabilities,claw}'
  #- '{capabilities,clawWorkspace}'
WHERE concept = 'v1:agents:agent'
  AND jsonb_typeof(payload -> 'capabilities') = 'object'
  AND (payload -> 'capabilities') ?| array['avatar', 'lipSync', 'vision', 'voiceToVoice', 'claw', 'clawWorkspace'];

UPDATE "MemoryNodes"
SET payload = payload #- '{providerConfig,voice}' #- '{providerConfig,avatar}'
WHERE concept = 'v1:agents:agent'
  AND jsonb_typeof(payload -> 'providerConfig') = 'object'
  AND (payload -> 'providerConfig') ?| array['voice', 'avatar'];

UPDATE "MemoryNodes"
SET payload = payload #- '{providerConfig,llm,policyName}'
WHERE concept = 'v1:agents:agent'
  AND jsonb_typeof(payload #> '{providerConfig,llm}') = 'object'
  AND (payload #> '{providerConfig,llm}') ? 'policyName';

UPDATE "MemoryNodes"
SET payload = payload #- '{lineage,originatingPlanId}'
WHERE concept = 'v1:agents:agent'
  AND jsonb_typeof(payload -> 'lineage') = 'object'
  AND (payload -> 'lineage') ? 'originatingPlanId';

UPDATE "MemoryNodes"
SET payload = payload - 'recommendedGender'
WHERE concept = 'v1:agents:agentRole'
  AND payload ? 'recommendedGender';

UPDATE "MemoryNodes"
SET payload = payload - 'planId'
WHERE concept = 'v1:agents:skillChangeEvent'
  AND payload ? 'planId';
