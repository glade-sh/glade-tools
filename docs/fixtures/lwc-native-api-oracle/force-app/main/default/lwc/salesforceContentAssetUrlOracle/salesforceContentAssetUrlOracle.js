import { LightningElement } from "lwc";
import value from "@salesforce/contentAssetUrl/GladeLwcOracleAsset";

export default class Oracle extends LightningElement {
  label = "@salesforce/contentAssetUrl/";
  value = value;
}
