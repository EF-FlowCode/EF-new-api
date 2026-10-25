package jsplugin

// DedicatedChannelType is the task-plugin channel identifier used by offline
// contract validation. It matches upstream ChannelTypeTaskPlugin (61); eflow used
// 62 before the 2026-10 renumbering moved its Ali media types to 1001–1004.
// Production channel registration belongs to a later phase.
const DedicatedChannelType = 61

// MaxTaskDurationSeconds matches the upstream v1 usage-example contract.
// It does not change duration validation on eflow's native task routes.
const MaxTaskDurationSeconds = 3600
