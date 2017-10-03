import React, {PropTypes} from 'react'

import {emptyGraphCopy} from 'src/shared/copy/cell'

import AutoRefresh from 'shared/components/AutoRefresh'
import LineGraph from 'shared/components/LineGraph'
import SingleStat from 'shared/components/SingleStat'

const RefreshingLineGraph = AutoRefresh(LineGraph)
const RefreshingSingleStat = AutoRefresh(SingleStat)

const RefreshingGraph = ({
  axes,
  type,
  query,
  queries,
  onZoom,
  templates,
  timeRange,
  cellHeight,
  autoRefresh,
  synchronizer,
  resizeCoords,
  editQueryStatus,
}) => {
  if (!query) {
    return (
      <div className="graph-empty">
        <p data-test="data-explorer-no-results">
          {emptyGraphCopy}
        </p>
      </div>
    )
  }

  if (type === 'single-stat') {
    return (
      <RefreshingSingleStat
        queries={query || queries}
        templates={templates}
        autoRefresh={autoRefresh}
        cellHeight={cellHeight}
      />
    )
  }

  const displayOptions = {
    stepPlot: type === 'line-stepplot',
    stackedGraph: type === 'line-stacked',
  }

  return (
    <RefreshingLineGraph
      axes={axes}
      query={query}
      queries={queries}
      onZoom={onZoom}
      templates={templates}
      timeRange={timeRange}
      autoRefresh={autoRefresh}
      isBarGraph={type === 'bar'}
      synchronizer={synchronizer}
      resizeCoords={resizeCoords}
      displayOptions={displayOptions}
      editQueryStatus={editQueryStatus}
      showSingleStat={type === 'line-plus-single-stat'}
    />
  )
}

const {arrayOf, func, number, shape, string} = PropTypes

RefreshingGraph.propTypes = {
  timeRange: shape({
    lower: string.isRequired,
  }),
  autoRefresh: number.isRequired,
  templates: arrayOf(shape()),
  synchronizer: func,
  type: string.isRequired,
  cellHeight: number,
  axes: shape(),
  query: shape(),
  queries: arrayOf(shape()),
  editQueryStatus: func,
  onZoom: func,
  resizeCoords: shape(),
}

export default RefreshingGraph
