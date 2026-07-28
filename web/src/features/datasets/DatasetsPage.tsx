import { useState } from "react";
import { useParams } from "react-router-dom";
import { Col, Row, Skeleton } from "antd";
import { useDatasets } from "../../api/hooks";
import { CasesTable } from "./CasesTable";
import { DatasetsPanel } from "./DatasetsPanel";
import { PageHeader } from "../../components/ui/PageHeader";
import { PageSection } from "../../components/ui/PageSection";
import { EmptyState } from "../../components/ui/EmptyState";
import { TechnicalId } from "../../components/ui/TechnicalId";
import type { Dataset } from "../../api/types";


export function DatasetsPage() {
  const { projectId = "" } = useParams<{ projectId: string }>();
  const { data: datasets = [], isLoading } = useDatasets(projectId);
  const [activeDataset, setActiveDataset] = useState<Dataset | null>(null);
  return <div>
    <PageHeader eyebrow="质量资产" title="数据集" description="统一管理手工用例与从线上 Gold 回灌的高价值样本。" />
    <Row gutter={[16, 16]}>
      <Col xs={24} xl={8}><DatasetsPanel projectId={projectId} onCreated={setActiveDataset} />
        <PageSection title="已有数据集" description="选择一个数据集查看和补充 Case" className="datasets-picker" >
          {isLoading ? <Skeleton active paragraph={{ rows: 4 }} /> : datasets.length ? <div className="surface-grid">{datasets.map((dataset) => <button type="button" className={`signal-card ${activeDataset?.id === dataset.id ? "signal-card--success" : ""}`} key={dataset.id} onClick={() => setActiveDataset(dataset)}><span className="signal-card__label">{dataset.name}</span><span className="signal-card__hint">{dataset.description || "未填写描述"}</span></button>)}</div> : <EmptyState title="先创建一个数据集" description="数据集用于承载回归用例和在线标注回流。" />}
        </PageSection>
      </Col>
      <Col xs={24} xl={16}><PageSection title={activeDataset ? `用例 · ${activeDataset.name}` : "用例"} description={activeDataset ? <span>数据集 ID：<TechnicalId value={activeDataset.id} /></span> : "从左侧选择一个数据集开始管理用例。"}>
        {activeDataset ? <CasesTable datasetId={activeDataset.id} /> : <EmptyState title="尚未选择数据集" description="创建或选择数据集后，可添加输入、期望输出并追溯线上来源。" />}
      </PageSection></Col>
    </Row>
  </div>;
}
