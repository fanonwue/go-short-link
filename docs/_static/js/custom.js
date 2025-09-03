'use strict'
const detailsTooltip = 'Click to expand'

document.addEventListener( 'DOMContentLoaded', function() {
    document.querySelectorAll('details.collapse summary').forEach(el => {
        el.setAttribute('title', detailsTooltip)
    })

    /*
    document.querySelectorAll('a.reference.external').forEach(el => {
        el.setAttribute('target', '_blank')
    })
     */
})