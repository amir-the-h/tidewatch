// Entity types shared by the Tracker tabs. '' is a custom (untyped) entity.
export const entityTypeOptions = [
  { label: 'LOC — Location',     value: 'LOC'  },
  { label: 'PER — Person',       value: 'PER'  },
  { label: 'ORG — Organization', value: 'ORG'  },
  { label: 'MISC — Misc',        value: 'MISC' },
  { label: 'Custom',             value: ''     },
]

// Filter options: the named types only.
export const entityTypeFilterOptions = entityTypeOptions.filter(o => o.value)
